package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

type VesselGraph struct {
	driver neo4j.DriverWithContext
	dbName string
}

// NewVesselGraph initializes the Neo4j connection and ensures the Vector Index exists.
func NewVesselGraph(uri, user, pass, dbName string) (*VesselGraph, error) {
	driver, err := neo4j.NewDriverWithContext(uri, neo4j.BasicAuth(user, pass, ""))
	if err != nil {
		return nil, fmt.Errorf("failed to create driver: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := driver.VerifyConnectivity(ctx); err != nil {
		return nil, fmt.Errorf("neo4j connectivity failed: %w", err)
	}

	v := &VesselGraph{driver: driver, dbName: dbName}

	// Ensure the Knowledge Mesh is ready
	if err := v.ensureIndexes(ctx); err != nil {
		return nil, err
	}

	return v, nil
}

func (v *VesselGraph) ensureIndexes(ctx context.Context) error {
	// Create Vector Index for 1536-D embeddings
	query := `
	CREATE VECTOR INDEX shard_embeddings IF NOT EXISTS
	FOR (s:Shard) ON (s.embedding)
	OPTIONS {indexConfig: {
		` + "`vector.dimensions`" + `: 1536,
		` + "`vector.similarity_function`" + `: 'cosine'
	}}`

	_, err := neo4j.ExecuteQuery(ctx, v.driver, query, nil, neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithDatabase(v.dbName))
	return err
}

func (v *VesselGraph) SaveShard(ctx context.Context, s Shard) error {
	query := `
	MERGE (s:Shard {id: $id})
	SET	s.category = $category,
			s.content = $content,
			s.embedding = $embedding,
			s.metadata = $metadata,
			s.source_type = $source_type,
			s.source_ref = $source_ref,
			s.confidence = $confidence,
			s.last_used = $last_used,
			s.created_at = $created_at
	`
	params := map[string]any{
		"id":          s.ID,
		"category":    s.Category,
		"content":     s.Content,
		"embedding":   decodeVector(s.Vector), // Convert []byte to []float32 for Neo4j
		"metadata":    string(s.Metadata),
		"source_type": s.SourceType,
		"source_ref":  s.SourceRef,
		"confidence":  s.Confidence,
		"last_used":   s.LastUsed.Format(time.RFC3339),
		"created_at":  s.CreatedAt.Format(time.RFC3339),
	}

	_, err := neo4j.ExecuteQuery(ctx, v.driver, query, params, neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithDatabase(v.dbName))
	return err
}

func (v *VesselGraph) FindResonant(ctx context.Context, queryVector []byte, limit int) ([]Shard, error) {
	query := `
	CALL db.index.vector.queryNodes('shard_embeddings', $limit, $vector)
	YIELD node, score
	RETURN node
	`

	params := map[string]any{
		"limit":  limit,
		"vector": decodeVector(queryVector),
	}

	result, err := neo4j.ExecuteQuery(ctx, v.driver, query, params, neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithDatabase(v.dbName))
	if err != nil {
		return nil, err
	}

	var shards []Shard
	for _, record := range result.Records {
		node, _ := record.Get("node")
		shards = append(shards, nodeToShard(node.(neo4j.Node)))
	}
	return shards, nil
}

// GetEvictionCandidates identifies "Orphan" shards (low centrality/links) using PageRank
func (v *VesselGraph) GetEvictionCandidates(ctx context.Context, limit int) ([]string, error) {
	// 1. Try PageRank via GDS
	const projectionQuery = `
	CALL gds.graph.project('janitorGraph', 'Shard', 'CONNECTED_TO', {
		relationshipProperties: 'weight'
	}) YIELD graphName
	`
	const pageRankQuery = `
	CALL gds.pageRank.stream('janitorGraph', {
		relationshipWeightProperty: 'weight'
	})
	YIELD nodeId, score
	WITH gds.util.asNode(nodeId) AS s, score
	WHERE s.category <> 'core'
	RETURN s.id
	ORDER BY score ASC, s.last_used ASC
	LIMIT $limit
	`
	const dropQuery = `CALL gds.graph.drop('janitorGraph', false) YIELD graphName`

	// We use a transaction to ensure cleanup
	session := v.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: v.dbName})
	defer session.Close(ctx)

	var ids []string
	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		// Create projection
		_, err := tx.Run(ctx, projectionQuery, nil)
		if err != nil {
			return nil, err
		}

		// Run PageRank
		res, err := tx.Run(ctx, pageRankQuery, map[string]any{"limit": limit})
		if err != nil {
			return nil, err
		}

		for res.Next(ctx) {
			id, _ := res.Record().Get("s.id")
			ids = append(ids, id.(string))
		}

		// Drop projection
		_, _ = tx.Run(ctx, dropQuery, nil)
		return nil, nil
	})

	if err == nil && len(ids) > 0 {
		return ids, nil
	}

	// 2. Fallback to Degree Centrality if GDS fails or no links exist
	fallbackQuery := `
	MATCH (s:Shard)
	WHERE s.category <> 'core'
	OPTIONAL MATCH (s)-[r]-()
	WITH s, count(r) as links
	ORDER BY links ASC, s.last_used ASC
	LIMIT $limit
	RETURN s.id
	`
	params := map[string]any{"limit": limit}

	result, err := neo4j.ExecuteQuery(ctx, v.driver, fallbackQuery, params, neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithDatabase(v.dbName))
	if err != nil {
		return nil, err
	}

	ids = nil
	for _, record := range result.Records {
		id, _ := record.Get("s.id")
		ids = append(ids, id.(string))
	}
	return ids, nil
}

// Helpers

func (v *VesselGraph) CalculateCommunities(ctx context.Context) (int, error) {
	const cleanupQuery = `CALL gds.graph.drop('communityGraph', false)`
	const projectQuery = `
	CALL gds.graph.project('communityGraph', 'Shard', 'CONNECTED_TO', {
		relationshipProperties: 'weight'
	}) YIELD graphName
	`
	const louvainQuery = `
	CALL gds.louvain.write('communityGraph', {
		writeProperty: 'community'
	}) YIELD communityCount
	RETURN communityCount
	`
	const pageRankQuery = `
	CALL gds.pageRank.write('communityGraph', {
		writeProperty: 'pagerank'
	}) YIELD computeMillis
	RETURN computeMillis
	`
	const dropQuery = `CALL gds.graph.drop('communityGraph', false)`

	session := v.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: v.dbName})
	defer session.Close(ctx)

	var count int
	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		_, _ = tx.Run(ctx, cleanupQuery, nil) // Ensure clean state
		_, err := tx.Run(ctx, projectQuery, nil)
		if err != nil {
			return nil, err
		}

		res, err := tx.Run(ctx, louvainQuery, nil)
		if err != nil {
			return nil, err
		}

		if res.Next(ctx) {
			val, _ := res.Record().Get("communityCount")
			count = int(val.(int64))
		}

		// Calculate PageRank as well
		_, err = tx.Run(ctx, pageRankQuery, nil)
		if err != nil {
			return nil, err
		}

		_, _ = tx.Run(ctx, dropQuery, nil)
		return nil, nil
	})

	return count, err
}

func nodeToShard(node neo4j.Node) Shard {
	props := node.GetProperties()

	id, _ := props["id"].(string)
	cat, _ := props["category"].(string)
	cont, _ := props["content"].(string)
	meta, _ := props["metadata"].(string)

	// Phase 9: Source Provenance
	srcType, _ := props["source_type"].(string)
	srcRef, _ := props["source_ref"].(string)
	conf, _ := props["confidence"].(float64)

	luStr, _ := props["last_used"].(string)
	caStr, _ := props["created_at"].(string)

	lu, _ := time.Parse(time.RFC3339, luStr)
	ca, _ := time.Parse(time.RFC3339, caStr)

	// Extract Community ID
	commID, _ := props["community"].(int64)

	// Convert []float32 (Neo4j) back to []byte (Shard-Link)
	var vec []byte
	if rawVec, ok := props["embedding"].([]any); ok {
		vec = make([]byte, len(rawVec)*4)
		for i, v := range rawVec {
			f := float32(v.(float64))
			putFloat32(vec[i*4:], f)
		}
	}

	return Shard{
		ID:          id,
		Category:    cat,
		Content:     cont,
		Metadata:    []byte(meta),
		Vector:      vec,
		SourceType:  srcType,
		SourceRef:   srcRef,
		Confidence:  conf,
		CommunityID: commID,
		LastUsed:    lu,
		CreatedAt:   ca,
	}
}

// Implement the rest of the interface (GetAllShards, GetCount, Close, etc.) to satisfy Repository
func (v *VesselGraph) Close() error {
	return v.driver.Close(context.Background())
}

func (v *VesselGraph) GetCount(ctx context.Context) (int, error) {
	query := "MATCH (s:Shard) RETURN count(s) as count"
	res, err := neo4j.ExecuteQuery(ctx, v.driver, query, nil, neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithDatabase(v.dbName))
	if err != nil {
		return 0, err
	}
	count, _ := res.Records[0].Get("count")
	return int(count.(int64)), nil
}

func (v *VesselGraph) ArchiveShard(ctx context.Context, id string) error {
	query := "MATCH (s:Shard {id: $id}) DETACH DELETE s" // Simple deletion for now
	_, err := neo4j.ExecuteQuery(ctx, v.driver, query, map[string]any{"id": id}, neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithDatabase(v.dbName))
	return err
}

func (v *VesselGraph) SaveBond(ctx context.Context, b ShardBond) error {
	query := `
	MATCH (from:Shard {id: $fromID})
	MATCH (to:Shard {id: $toID})
	MERGE (from)-[r:CONNECTED_TO]->(to)
	SET r.weight = $weight
	`
	params := map[string]any{
		"fromID": b.FromID,
		"toID":   b.ToID,
		"weight": b.Weight,
	}

	_, err := neo4j.ExecuteQuery(ctx, v.driver, query, params, neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithDatabase(v.dbName))
	return err
}

func (v *VesselGraph) GetAllBonds(ctx context.Context) ([]ShardBond, error) {
	query := `
	MATCH (from:Shard)-[r:CONNECTED_TO]->(to:Shard)
	RETURN from.id, to.id, r.weight
	`
	result, err := neo4j.ExecuteQuery(ctx, v.driver, query, nil, neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithDatabase(v.dbName))
	if err != nil {
		return nil, err
	}

	var bonds []ShardBond
	for _, record := range result.Records {
		fID, _ := record.Get("from.id")
		tID, _ := record.Get("to.id")
		w, _ := record.Get("r.weight")
		bonds = append(bonds, ShardBond{
			FromID: fID.(string),
			ToID:   tID.(string),
			Weight: w.(float64),
		})
	}
	return bonds, nil
}

func (v *VesselGraph) GetAllShards(ctx context.Context) ([]Shard, error) {
	query := "MATCH (s:Shard) RETURN s"
	result, err := neo4j.ExecuteQuery(ctx, v.driver, query, nil, neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithDatabase(v.dbName))
	if err != nil {
		return nil, err
	}

	var shards []Shard
	for _, record := range result.Records {
		node, _ := record.Get("s")
		shards = append(shards, nodeToShard(node.(neo4j.Node)))
	}
	return shards, nil
}

func (v *VesselGraph) FindText(ctx context.Context, query string, limit int) ([]Shard, error) {
	cypher := `
	MATCH (s:Shard)
	WHERE s.content CONTAINS $query
	RETURN s
	ORDER BY s.last_used DESC
	LIMIT $limit
	`
	params := map[string]any{
		"query": query,
		"limit": limit,
	}
	result, err := neo4j.ExecuteQuery(ctx, v.driver, cypher, params, neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithDatabase(v.dbName))
	if err != nil {
		return nil, err
	}

	var shards []Shard
	for _, record := range result.Records {
		node, _ := record.Get("s")
		shards = append(shards, nodeToShard(node.(neo4j.Node)))
	}
	return shards, nil
}

// FindHybrid performs Reciprocal Rank Fusion (RRF) on vector and text search results.
func (v *VesselGraph) FindHybrid(ctx context.Context, textQuery string, queryVector []byte, limit int) ([]Shard, error) {
	candidateLimit := limit * 2

	vectorResults, err := v.FindResonant(ctx, queryVector, candidateLimit)
	if err != nil {
		return nil, fmt.Errorf("vector search failed in hybrid: %w", err)
	}

	textResults, err := v.FindText(ctx, textQuery, candidateLimit)
	if err != nil {
		return nil, fmt.Errorf("text search failed in hybrid: %w", err)
	}

	return ReciprocalRankFusion(limit, 60.0, vectorResults, textResults), nil
}

func (v *VesselGraph) GetCoreShards(ctx context.Context) ([]Shard, error) {
	query := "MATCH (s:Shard {category: 'core'}) RETURN s"
	result, err := neo4j.ExecuteQuery(ctx, v.driver, query, nil, neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithDatabase(v.dbName))
	if err != nil {
		return nil, err
	}

	var shards []Shard
	for _, record := range result.Records {
		node, _ := record.Get("s")
		shards = append(shards, nodeToShard(node.(neo4j.Node)))
	}
	return shards, nil
}

func (v *VesselGraph) SearchGraph(ctx context.Context, queryVector []byte, limit int) ([]Shard, error) {
	// Find the closest shard, then get its neighbors (Multi-Hop)
	query := `
	CALL db.index.vector.queryNodes('shard_embeddings', 1, $vector)
	YIELD node AS center, score
	MATCH (center)-[:CONNECTED_TO]-(neighbor:Shard)
	RETURN neighbor
	LIMIT $limit
	`
	params := map[string]any{
		"vector": decodeVector(queryVector),
		"limit":  limit,
	}

	result, err := neo4j.ExecuteQuery(ctx, v.driver, query, params, neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithDatabase(v.dbName))
	if err != nil {
		return nil, err
	}

	var shards []Shard
	for _, record := range result.Records {
		node, _ := record.Get("neighbor")
		shards = append(shards, nodeToShard(node.(neo4j.Node)))
	}
	return shards, nil
}

func (v *VesselGraph) GetGraphData(ctx context.Context) ([]Shard, []ShardBond, error) {
	shards, err := v.GetAllShards(ctx)
	if err != nil {
		return nil, nil, err
	}
	bonds, err := v.GetAllBonds(ctx)
	if err != nil {
		return nil, nil, err
	}
	return shards, bonds, nil
}

func (v *VesselGraph) FindInvalidShards(ctx context.Context) ([]string, error) {
	query := `
	MATCH (s:Shard)
	WHERE s.embedding IS NULL OR size(s.embedding) = 0
	RETURN s.id AS id
	`

	result, err := neo4j.ExecuteQuery(ctx, v.driver, query, nil, neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithDatabase(v.dbName))
	if err != nil {
		return nil, err
	}

	var ids []string
	for _, record := range result.Records {
		id, _ := record.Get("id")
		ids = append(ids, id.(string))
	}
	return ids, nil
}

func (v *VesselGraph) FindOrphanShards(ctx context.Context) ([]string, error) {
	query := `
	MATCH (s:Shard)
	WHERE s.category <> 'core'
		AND NOT (s)-[:CONNECTED_TO]-()
	RETURN s.id AS id
	`
	result, err := neo4j.ExecuteQuery(ctx, v.driver, query, nil, neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithDatabase(v.dbName))
	if err != nil {
		return nil, err
	}

	var ids []string
	for _, record := range result.Records {
		id, _ := record.Get("id")
		ids = append(ids, id.(string))
	}
	return ids, nil
}

// Optimize performs Neo4j maintenance (mostly a no-op as Neo4j handles it)
func (v *VesselGraph) Optimize(ctx context.Context) error {
	// Neo4j handles space reclamation internally.
	// Our Vector index is ensured on startup, but we can call it here just to be safe.
	return v.ensureIndexes(ctx)
}
