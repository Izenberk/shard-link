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
			s.last_used = $last_used,
			s.created_at = $created_at
	`
	params := map[string]any{
		"id":         s.ID,
		"category":   s.Category,
		"content":    s.Content,
		"embedding":  decodeVector(s.Vector), // Convert []byte to []float32 for Neo4j
		"metadata":   string(s.Metadata),
		"last_used":  s.LastUsed.Format(time.RFC3339),
		"created_at": s.CreatedAt.Format(time.RFC3339),
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

// GetEvictionCandidates identifies "Orphan" shards (low centrality/links)
func (v *VesselGraph) GetEvictionCandidates(ctx context.Context, limit int) ([]string, error) {
	query := `
	MATCH (s:Shard)
	WHERE s.category <> 'core'
	OPTIONAL MATCH (s)-[r]-()
	WITH s, count(r) as links
	ORDER BY links ASC, s.last_used ASC
	LIMIT $limit
	RETURN s.id
	`

	params := map[string]any{"limit": limit}

	result, err := neo4j.ExecuteQuery(ctx, v.driver, query, params, neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithDatabase(v.dbName))
	if err != nil {
		return nil, err
	}

	var ids []string
	for _, record := range result.Records {
		id, _ := record.Get("s.id")
		ids = append(ids, id.(string))
	}
	return ids, nil
}

// Helpers

func nodeToShard(node neo4j.Node) Shard {
	props := node.GetProperties()
	
	id, _ := props["id"].(string)
	cat, _ := props["category"].(string)
	cont, _ := props["content"].(string)
	meta, _ := props["metadata"].(string)
	
	luStr, _ := props["last_used"].(string)
	caStr, _ := props["created_at"].(string)
	
	lu, _ := time.Parse(time.RFC3339, luStr)
	ca, _ := time.Parse(time.RFC3339, caStr)

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
		ID:        id,
		Category:  cat,
		Content:   cont,
		Metadata:  []byte(meta),
		Vector:    vec,
		LastUsed:  lu,
		CreatedAt: ca,
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
