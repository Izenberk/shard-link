package storage

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
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

func (v *VesselGraph) ExecuteQuery(ctx context.Context, query string, params map[string]any) (*neo4j.EagerResult, error) {
	return neo4j.ExecuteQuery(ctx, v.driver, query, params, neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithDatabase(v.dbName))
}

func (v *VesselGraph) ensureIndexes(ctx context.Context) error {
	// 1. Vector Index for 3072-D embeddings (Production Gemini Standard)
	vectorQuery := `
	CREATE VECTOR INDEX shard_embeddings IF NOT EXISTS
	FOR (s:Shard) ON (s.embedding)
	OPTIONS {indexConfig: {
		` + "`vector.dimensions`" + `: 3072,
		` + "`vector.similarity_function`" + `: 'cosine'
	}}`

	if _, err := neo4j.ExecuteQuery(ctx, v.driver, vectorQuery, nil, neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithDatabase(v.dbName)); err != nil {
		return err
	}

	// 2. Full-Text Index for Keyword Retrieval (Global Scale)
	ftQuery := `
	CREATE FULLTEXT INDEX shard_content_ft IF NOT EXISTS
	FOR (s:Shard) ON EACH [s.content]
	`
	_, err := neo4j.ExecuteQuery(ctx, v.driver, ftQuery, nil, neo4j.EagerResultTransformer,
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
	if err == nil && GlobalLogger != nil {
		GlobalLogger(fmt.Sprintf("Shard Saved: %s", s.ID), "success", s.ID)
	}
	log.Printf("[Vessel] Shard Saved: %s (Category: %s)", s.ID, s.Category)

	// Aha! Moment: Immediate Associative Linking (Phase 11)
	tStr := os.Getenv("MESH_LINK_THRESHOLD")
	threshold, err := strconv.ParseFloat(tStr, 64)
	if err != nil {
		threshold = 0.75
	}

	linkQuery := `
	MATCH (new:Shard {id: $id})
	MATCH (existing:Shard)
	WHERE new.id <> existing.id
	  AND new.category <> 'archived'
	  AND existing.category <> 'archived'
	  AND size(existing.embedding) = 3072
	WITH new, existing, gds.similarity.cosine(new.embedding, existing.embedding) AS sim
	WHERE sim > $threshold
	MERGE (new)-[r:CONNECTED_TO]-(existing)
	SET r.weight = sim
	`
	_, _ = neo4j.ExecuteQuery(ctx, v.driver, linkQuery, map[string]any{
		"id":        s.ID,
		"threshold": threshold,
	}, neo4j.EagerResultTransformer, neo4j.ExecuteQueryWithDatabase(v.dbName))

	return nil
}

func (v *VesselGraph) FindResonant(ctx context.Context, queryVector []byte, limit int, shouldTouch bool) ([]Shard, error) {
	var query string
	if shouldTouch {
		query = `
		CALL db.index.vector.queryNodes('shard_embeddings', $limit, $vector)
		YIELD node, score
		SET node.last_used = datetime(),
			node.use_count = coalesce(node.use_count, 0) + 1
		RETURN node
		`
	} else {
		query = `
		CALL db.index.vector.queryNodes('shard_embeddings', $limit, $vector)
		YIELD node, score
		RETURN node
		`
	}

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

// ReinforceShards manually updates usage metrics for a list of shard IDs.
// This prevents "double-touching" during meta-tool operations.
func (v *VesselGraph) ReinforceShards(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	query := `
	MATCH (s:Shard)
	WHERE s.id IN $ids
	SET s.last_used = datetime(),
	    s.use_count = coalesce(s.use_count, 0) + 1
	`
	_, err := neo4j.ExecuteQuery(ctx, v.driver, query, map[string]any{"ids": ids}, neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithDatabase(v.dbName))
	return err
}


// GetEvictionCandidates identifies "Orphan" shards (low centrality/links) using PageRank
func (v *VesselGraph) GetEvictionCandidates(ctx context.Context, limit int) ([]string, error) {
	threshold := os.Getenv("JANITOR_RESONANCE_THRESHOLD")
	if threshold == "" {
		threshold = "0.70"
	}

	// 1. Try PageRank via GDS + Core Resonance protection
	const projectionQuery = `
	CALL gds.graph.project('janitorGraph', 'Shard', 'CONNECTED_TO', {
		relationshipProperties: 'weight'
	}) YIELD graphName
	`
	const pageRankQuery = `
	MATCH (core:Shard {category: 'core'})
	MATCH (s:Shard) WHERE s.category <> 'core'
	WITH s, core, gds.similarity.cosine(s.embedding, core.embedding) as sim
	WITH s, max(sim) as maxResonance
	WHERE maxResonance < parseFloat($threshold)
	
	CALL gds.pageRank.stream('janitorGraph', {
		relationshipWeightProperty: 'weight'
	})
	YIELD nodeId, score
	WHERE gds.util.asNode(nodeId).id = s.id
	
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

		// Run PageRank with Resonance filtering
		res, err := tx.Run(ctx, pageRankQuery, map[string]any{"limit": limit, "threshold": threshold})
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

	// 2. Fallback to Degree Centrality + Core Resonance protection if GDS fails or no links exist
	fallbackQuery := `
	MATCH (core:Shard {category: 'core'})
	MATCH (s:Shard) WHERE s.category <> 'core'
	WITH s, core, gds.similarity.cosine(s.embedding, core.embedding) as sim
	WITH s, max(sim) as maxResonance
	WHERE maxResonance < parseFloat($threshold)

	OPTIONAL MATCH (s)-[r]-()
	WITH s, count(r) as links
	ORDER BY links ASC, s.last_used ASC
	LIMIT $limit
	RETURN s.id
	`
	params := map[string]any{"limit": limit, "threshold": threshold}

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
	
	var conf float64
	if c, ok := props["confidence"].(float64); ok {
		conf = c
	} else if c, ok := props["confidence"].(int64); ok {
		conf = float64(c)
	}

	luStr, _ := props["last_used"].(string)
	caStr, _ := props["created_at"].(string)

	lu, _ := time.Parse(time.RFC3339, luStr)
	ca, _ := time.Parse(time.RFC3339, caStr)

	var useCount int
	if uc, ok := props["use_count"].(int64); ok {
		useCount = int(uc)
	} else if uc, ok := props["use_count"].(float64); ok {
		useCount = int(uc)
	}

	// Extract Community ID and PageRank
	var commID int64
	if c, ok := props["community"].(int64); ok {
		commID = c
	} else if c, ok := props["community"].(float64); ok {
		commID = int64(c)
	}

	var rank float64
	if r, ok := props["pagerank"].(float64); ok {
		rank = r
	} else if r, ok := props["pagerank"].(int64); ok {
		rank = float64(r)
	}

	// Convert []float32 (Neo4j) back to []byte (Shard-Link)
	var vec []byte
	if rawVec, ok := props["embedding"].([]any); ok {
		vec = make([]byte, len(rawVec)*4)
		for i, v := range rawVec {
			var f float32
			switch val := v.(type) {
			case float64:
				f = float32(val)
			case int64:
				f = float32(val)
			}
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
		PageRank:    rank,
		LastUsed:    lu,
		CreatedAt:   ca,
		UseCount:    useCount,
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
	// Note: In the Triple-Engine model, the caller is responsible for moving 
	// data to Postgres before calling this to remove it from the "Living Mesh" (Neo4j).
	query := "MATCH (s:Shard {id: $id}) DETACH DELETE s"
	_, err := neo4j.ExecuteQuery(ctx, v.driver, query, map[string]any{"id": id}, neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithDatabase(v.dbName))
	if err == nil {
		if GlobalLogger != nil {
			GlobalLogger(fmt.Sprintf("Shard Evicted: %s", id), "evict", id)
		}
		log.Printf("[Vessel] Shard Evicted/Deleted from Mesh: %s", id)
	}
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
	if err == nil {
		if GlobalLogger != nil {
			GlobalLogger(fmt.Sprintf("Bond Forged: %s <-> %s", b.FromID, b.ToID), "bond", b.FromID)
		}
		log.Printf("[Vessel] Bond Forged: %s <-> %s (w: %.2f)", b.FromID, b.ToID, b.Weight)
	}
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
	query := `
	MATCH (s:Shard) 
	OPTIONAL MATCH (s)-[r:CONNECTED_TO]-()
	RETURN s, count(r) as degree
	`
	result, err := neo4j.ExecuteQuery(ctx, v.driver, query, nil, neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithDatabase(v.dbName))
	if err != nil {
		return nil, err
	}

	var shards []Shard
	for _, record := range result.Records {
		node, _ := record.Get("s")
		degree, _ := record.Get("degree")
		shard := nodeToShard(node.(neo4j.Node))
		shard.BondCount = int(degree.(int64))
		shards = append(shards, shard)
	}
	return shards, nil
}

func (v *VesselGraph) FindText(ctx context.Context, query string, limit int, shouldTouch bool) ([]Shard, error) {
	// Super Senior Update: Use Full-Text Index instead of rigid CONTAINS
	// This handles tokenization, case-insensitivity, and ranking.
	// Unified Reinforcement: Update usage metrics so keyword search counts as a "touch".
	var cypher string
	if shouldTouch {
		cypher = `
		CALL db.index.fulltext.queryNodes('shard_content_ft', $query)
		YIELD node, score
		SET node.last_used = datetime(),
			node.use_count = coalesce(node.use_count, 0) + 1
		RETURN node
		LIMIT $limit
		`
	} else {
		cypher = `
		CALL db.index.fulltext.queryNodes('shard_content_ft', $query)
		YIELD node, score
		RETURN node
		LIMIT $limit
		`
	}
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
		node, _ := record.Get("node")
		shards = append(shards, nodeToShard(node.(neo4j.Node)))
	}
	return shards, nil
}

// FindHybrid performs Reciprocal Rank Fusion (RRF) on vector and text search results.
func (v *VesselGraph) FindHybrid(ctx context.Context, textQuery string, queryVector []byte, limit int) ([]Shard, error) {
	candidateLimit := limit * 2

	vectorResults, err := v.FindResonant(ctx, queryVector, candidateLimit, true)
	if err != nil {
		return nil, fmt.Errorf("vector search failed in hybrid: %w", err)
	}

	textResults, err := v.FindText(ctx, textQuery, candidateLimit, true)
	if err != nil {
		return nil, fmt.Errorf("text search failed in hybrid: %w", err)
	}

	return ReciprocalRankFusion(limit, 60.0, vectorResults, textResults), nil
}

func (v *VesselGraph) GetShardByID(ctx context.Context, id string) (Shard, error) {
	query := "MATCH (s:Shard {id: $id}) RETURN s"
	res, err := neo4j.ExecuteQuery(ctx, v.driver, query, map[string]any{"id": id}, neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithDatabase(v.dbName))
	if err != nil {
		return Shard{}, err
	}
	if len(res.Records) == 0 {
		return Shard{}, fmt.Errorf("shard %s not found in mesh", id)
	}
	node, _ := res.Records[0].Get("s")
	return nodeToShard(node.(neo4j.Node)), nil
}

func (v *VesselGraph) GetArchivedShards(ctx context.Context) ([]Shard, error) {
	return nil, nil // Neo4j is for living memory only
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

func (v *VesselGraph) SearchGraph(ctx context.Context, queryVector []byte, limit int, shouldTouch bool) ([]Shard, []ShardBond, error) {
	vec := decodeVector(queryVector)
	
	// Multi-Hop: Find the center AND its connected neighbors + relationships
	var query string
	if shouldTouch {
		query = `
		CALL db.index.vector.queryNodes('shard_embeddings', 1, $vector)
		YIELD node AS center, score
		SET center.last_used = datetime(),
			center.use_count = coalesce(center.use_count, 0) + 1
		WITH center
		OPTIONAL MATCH (center)-[r:CONNECTED_TO]-(neighbor:Shard)
		SET neighbor.last_used = datetime(),
			neighbor.use_count = coalesce(neighbor.use_count, 0) + 1
		WITH center, neighbor, r
		RETURN center, count(r) as centerDegree, collect({node: neighbor, weight: r.weight}) as neighbors
		`
	} else {
		query = `
		CALL db.index.vector.queryNodes('shard_embeddings', 1, $vector)
		YIELD node AS center, score
		WITH center
		OPTIONAL MATCH (center)-[r:CONNECTED_TO]-(neighbor:Shard)
		WITH center, neighbor, r
		RETURN center, count(r) as centerDegree, collect({node: neighbor, weight: r.weight}) as neighbors
		`
	}
	params := map[string]any{
		"vector": vec,
	}

	result, err := neo4j.ExecuteQuery(ctx, v.driver, query, params, neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithDatabase(v.dbName))
	if err != nil {
		return nil, nil, err
	}

	var shards []Shard
	var bonds []ShardBond
	seen := make(map[string]bool)
	
	if len(result.Records) > 0 {
		record := result.Records[0]
		centerNode, _ := record.Get("center")
		centerDegree, _ := record.Get("centerDegree")
		
		centerShard := nodeToShard(centerNode.(neo4j.Node))
		centerShard.BondCount = int(centerDegree.(int64))
		shards = append(shards, centerShard)
		seen[centerShard.ID] = true

		neighbors, _ := record.Get("neighbors")
		for _, nMap := range neighbors.([]any) {
			m := nMap.(map[string]any)
			neighborNode := m["node"]
			if neighborNode == nil {
				continue
			}
			
			neighborShard := nodeToShard(neighborNode.(neo4j.Node))
			// Note: neighborShard.BondCount is NOT populated here because it requires another query or a complex subquery.
			// However, since it's a neighbor, we know it has at least 1 bond (to the center).
			// For now, let's keep it simple and populate it in GetAllShards for the full graph view.
			
			if !seen[neighborShard.ID] {
				shards = append(shards, neighborShard)
				seen[neighborShard.ID] = true
			}
			
			// Capture the bond
			weight, _ := m["weight"].(float64)
			bonds = append(bonds, ShardBond{
				FromID: centerShard.ID,
				ToID:   neighborShard.ID,
				Weight: weight,
			})
		}
	}
	return shards, bonds, nil
}

func (v *VesselGraph) SyncBonds(ctx context.Context, threshold float64) (int, error) {
	query := `
	MATCH (s1:Shard)
	MATCH (s2:Shard)
	WHERE elementId(s1) < elementId(s2)
	  AND s1.category <> 'archived'
	  AND s2.category <> 'archived'
	  AND size(s1.embedding) = 3072 
	  AND size(s2.embedding) = 3072
	WITH s1, s2, gds.similarity.cosine(s1.embedding, s2.embedding) AS sim
	WHERE sim > $threshold
	MERGE (s1)-[r:CONNECTED_TO]->(s2)
	SET r.weight = sim
	RETURN count(r) as bondsCreated
	`
	params := map[string]any{
		"threshold": threshold,
	}

	result, err := neo4j.ExecuteQuery(ctx, v.driver, query, params, neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithDatabase(v.dbName))
	if err != nil {
		return 0, err
	}

	if len(result.Records) == 0 {
		return 0, nil
	}

	count, _ := result.Records[0].Get("bondsCreated")
	return int(count.(int64)), nil
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
