package storage

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

type VesselGraph struct {
	driver neo4j.DriverWithContext
	dbName string
}

// Community metrics cache for delta-write optimization
type CommunityMetrics struct {
	CommunityID		int64
	PageRank			float64
}

var (
	communityCache		map[string]CommunityMetrics
	communityCacheMu	sync.RWMutex
)

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
	// Read target dimension from env (must match what main.go sets)
	dim := 768
	if dimStr := os.Getenv("EMBEDDING_DIMENSION"); dimStr != "" {
		if d, err := strconv.Atoi(dimStr); err == nil && d > 0 {
			dim = d
		}
	}

	// 1. Check if existing vector index has a dimension mismatch
	checkQuery := `
	SHOW INDEXES
	WHERE name = 'shard_embeddings'
	RETURN properties(options) AS opts
	`
	checkRes, err := neo4j.ExecuteQuery(ctx, v.driver, checkQuery, nil, neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithDatabase(v.dbName))
	if err == nil && len(checkRes.Records) > 0 {
		// Index exists — check dimension
		needsRecreate := false
		if opts, ok := checkRes.Records[0].Get("opts"); ok {
			if m, ok := opts.(map[string]any); ok {
				if ic, ok := m["indexConfig"].(map[string]any); ok {
					if existingDim, ok := ic["vector.dimensions"].(int64); ok {
						if int(existingDim) != dim {
							slog.Warn("vector index dimension mismatch — recreating", "existing", existingDim, "target", dim)
							needsRecreate = true
						}
					}
				}
			}
		}
		if needsRecreate {
			dropQuery := `DROP INDEX shard_embeddings IF EXISTS`
			if _, err := neo4j.ExecuteQuery(ctx, v.driver, dropQuery, nil, neo4j.EagerResultTransformer,
				neo4j.ExecuteQueryWithDatabase(v.dbName)); err != nil {
				return fmt.Errorf("failed to drop old vector index: %w", err)
			}
		}
	}

	// 2. Create vector index at the target dimension
	vectorQuery := fmt.Sprintf(`
	CREATE VECTOR INDEX shard_embeddings IF NOT EXISTS
	FOR (s:Shard) ON (s.embedding)
	OPTIONS {indexConfig: {
		`+"`vector.dimensions`"+`: %d,
		`+"`vector.similarity_function`"+`: 'cosine'
	}}`, dim)

	if _, err := neo4j.ExecuteQuery(ctx, v.driver, vectorQuery, nil, neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithDatabase(v.dbName)); err != nil {
		return err
	}

	// 3. Full-Text Index for Keyword Retrieval (Global Scale)
	ftQuery := `
	CREATE FULLTEXT INDEX shard_content_ft IF NOT EXISTS
	FOR (s:Shard) ON EACH [s.content]
	`
	_, err = neo4j.ExecuteQuery(ctx, v.driver, ftQuery, nil, neo4j.EagerResultTransformer,
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
			s.last_used = CASE WHEN $last_used <> '0001-01-01T00:00:00Z' THEN $last_used WHEN s.last_used IS NOT NULL AND s.last_used <> '0001-01-01T00:00:00Z' THEN s.last_used ELSE $created_at END,
			s.created_at = CASE WHEN s.created_at IS NOT NULL AND s.created_at <> '0001-01-01T00:00:00Z' THEN s.created_at ELSE $created_at END,
			s.salience = $salience,
			s.retrieval_history = $retrieval_history
	`
	params := map[string]any{
		"id":                s.ID,
		"category":          s.Category,
		"content":           s.Content,
		"embedding":         DecodeVector(s.Vector), // Convert []byte to []float32 for Neo4j
		"metadata":          string(s.Metadata),
		"source_type":       s.SourceType,
		"source_ref":        s.SourceRef,
		"confidence":        s.Confidence,
		"last_used":         s.LastUsed.Format(time.RFC3339),
		"created_at":        s.CreatedAt.Format(time.RFC3339),
		"salience":          s.Salience,
		"retrieval_history": serializeRetrievalHistory(s.RetrievalHistory),
	}

	_, err := neo4j.ExecuteQuery(ctx, v.driver, query, params, neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithDatabase(v.dbName))
	if err == nil {
		if !strings.HasPrefix(s.ID, "comm-summary-") {
			MarkShardDirty()
		}
		if GlobalLogger != nil {
			GlobalLogger(fmt.Sprintf("Shard Saved: %s", s.ID), "success", s.ID)
		}
	}
	slog.Debug("shard saved", "id", s.ID, "category", s.Category)

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
	  AND size(existing.embedding) = size(new.embedding)
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
			node.use_count = coalesce(node.use_count, 0) + 1,
			node.retrieval_history = (coalesce(node.retrieval_history, []) + [toString(datetime())])[-20..]
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
		"vector": DecodeVector(queryVector),
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
	    s.use_count = coalesce(s.use_count, 0) + 1,
	    s.retrieval_history = (coalesce(s.retrieval_history, []) + [toString(datetime())])[-20..]
	`
	_, err := neo4j.ExecuteQuery(ctx, v.driver, query, map[string]any{"ids": ids}, neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithDatabase(v.dbName))
	return err
}


// GetEvictionCandidates identifies low-survival shards using the Survival Formula v4.1.
// Cypher handles hard exclusions (core protection, resonance filter).
// Go handles ranking: compute SurvivalScoreV4 for each candidate, sort ascending, return lowest N.
func (v *VesselGraph) GetEvictionCandidates(ctx context.Context, limit int) ([]string, error) {
	threshold := os.Getenv("JANITOR_RESONANCE_THRESHOLD")
	if threshold == "" {
		threshold = "0.70"
	}

	// Fetch all non-core shards that fail the resonance-to-core protection filter.
	// No ordering or limit in Cypher — Go handles ranking via SurvivalScoreV4.
	candidateQuery := `
	MATCH (core:Shard {category: 'core'})
	MATCH (s:Shard) WHERE s.category <> 'core' AND s.category <> 'community'
	WITH s, core, gds.similarity.cosine(s.embedding, core.embedding) as sim
	WITH s, max(sim) as maxResonance
	WHERE maxResonance < toFloat($threshold)
	OPTIONAL MATCH (s)-[r:CONNECTED_TO]-()
	RETURN s, count(r) as degree
	`
	params := map[string]any{"threshold": threshold}

	result, err := neo4j.ExecuteQuery(ctx, v.driver, candidateQuery, params, neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithDatabase(v.dbName))
	if err != nil {
		return nil, err
	}

	// Score each candidate using the full Survival Formula v4.1
	type scoredCandidate struct {
		id    string
		score float64
	}
	candidates := make([]scoredCandidate, 0, len(result.Records))

	for _, record := range result.Records {
		node, _ := record.Get("s")
		degree, _ := record.Get("degree")

		shard := nodeToShard(node.(neo4j.Node))
		bondCount := int(degree.(int64))

		score := SurvivalScoreV4(bondCount, shard.PageRank, shard.Salience, shard.RetrievalHistory, shard.LastUsed)
		candidates = append(candidates, scoredCandidate{id: shard.ID, score: score})

		slog.Debug("eviction candidate scored",
			"id", shard.ID, "survival", score, "bonds", bondCount, "pagerank", shard.PageRank, "salience", shard.Salience)
	}

	// Sort ascending — lowest survival scores get evicted first
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score < candidates[j].score
	})

	// Return the lowest N
	if limit > len(candidates) {
		limit = len(candidates)
	}
	ids := make([]string, limit)
	for i := 0; i < limit; i++ {
		ids[i] = candidates[i].id
	}
	return ids, nil
}

// Helpers

func (v *VesselGraph) CalculateCommunities(ctx context.Context) (int, []int64, error) {
	const cleanupQuery = `CALL gds.graph.drop('communityGraph', false)`
	const projectQuery = `
	CALL gds.graph.project.cypher('communityGraph',
		'MATCH (s:Shard) WHERE NOT s.id STARTS WITH "comm-summary-" AND s.category <> "contract" RETURN id(s) AS id',
		'MATCH (a:Shard)-[r:CONNECTED_TO]->(b:Shard) WHERE NOT a.id STARTS WITH "comm-summary-" AND a.category <> "contract" AND NOT b.id STARTS WITH "comm-summary-" AND b.category <> "contract" RETURN id(a) AS source, id(b) AS target, r.weight AS weight'
	) YIELD graphName`
	const louvainQuery = `
	CALL gds.louvain.stream('communityGraph')
	YIELD nodeId, communityId
	RETURN gds.util.asNode(nodeId).id AS shardID, communityId`
	const pageRankQuery = `
	CALL gds.pageRank.stream('communityGraph')
	YIELD nodeId, score
	RETURN gds.util.asNode(nodeId).id AS shardID, score`
	const dropQuery = `CALL gds.graph.drop('communityGraph', false)`

	session := v.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: v.dbName})
	defer session.Close(ctx)

	newCache := make(map[string]CommunityMetrics)

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		_, _ = tx.Run(ctx, cleanupQuery, nil)

		if _, err := tx.Run(ctx, projectQuery, nil); err != nil {
			return nil, err
		}

		lRes, err := tx.Run(ctx, louvainQuery, nil)
		if err != nil {
			return nil, err
		}
		for lRes.Next(ctx) {
			id, _ := lRes.Record().Get("shardID")
			comm, _ := lRes.Record().Get("communityId")
			newCache[id.(string)] = CommunityMetrics{CommunityID: comm.(int64)}
		}

		prRes, err := tx.Run(ctx, pageRankQuery, nil)
		if err != nil {
			return nil, err
		}
		for prRes.Next(ctx) {
			id, _ := prRes.Record().Get("shardID")
			score, _ := prRes.Record().Get("score")
			if m, ok := newCache[id.(string)]; ok {
				m.PageRank = score.(float64)
				newCache[id.(string)] = m
			}
		}

		_, _ = tx.Run(ctx, dropQuery, nil)
		return nil, nil
	})

	if err != nil {
		return 0, nil, err
	}

	// Delta write — only nodes whose values changed
	communityCacheMu.RLock()
	old := communityCache
	communityCacheMu.RUnlock()

	var updates []map[string]any
	for id, newM := range newCache {
		if old == nil || old[id] != newM {
			updates = append(updates, map[string]any{
				"id":        id,
				"community": newM.CommunityID,
				"pagerank":  newM.PageRank,
			})
		}
	}

	if len(updates) > 0 {
		writeQuery := `
		UNWIND $updates AS u
		MATCH (s:Shard {id: u.id})
		SET s.community = u.community, s.pagerank = u.pagerank`
		_, _ = neo4j.ExecuteQuery(ctx, v.driver, writeQuery,
			map[string]any{"updates": updates},
			neo4j.EagerResultTransformer,
			neo4j.ExecuteQueryWithDatabase(v.dbName))
		slog.Debug("community delta-write complete", "nodes_updated", len(updates))
	} else {
		slog.Debug("community delta-write: no changes")
	}

	communityCacheMu.Lock()
	communityCache = newCache
	communityCacheMu.Unlock()

	// Extract unique community IDs from the updates (changed communities only)
	seen := make(map[int64]bool)
	var changedCommunities []int64
	for _, u := range updates {
		cid := u["community"].(int64)
		if !seen[cid] {
			seen[cid] = true
			changedCommunities = append(changedCommunities, cid)
		}
	}

	return len(newCache), changedCommunities, nil
}

// PruneStaleSummaries removes comm-summary-* shards whose community ID
// no longer matches any active (non-summary) shard's community assignment.
// This prevents duplicate summaries from accumulating across Louvain reclustering runs.
func (v *VesselGraph) PruneStaleSummaries(ctx context.Context) (int, error) {
	query := `
	MATCH (active:Shard) WHERE NOT active.id STARTS WITH 'comm-summary-'
	WITH collect(DISTINCT active.community) AS activeCommunities
	MATCH (summary:Shard) WHERE summary.id STARTS WITH 'comm-summary-'
	WITH summary, activeCommunities,
	     toInteger(replace(summary.id, 'comm-summary-', '')) AS summaryCommID
	WHERE NOT summaryCommID IN activeCommunities
	DETACH DELETE summary
	RETURN count(*) AS pruned
	`

	result, err := neo4j.ExecuteQuery(ctx, v.driver, query, nil,
		neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithDatabase(v.dbName))
	if err != nil {
		return 0, fmt.Errorf("prune stale summaries: %w", err)
	}

	if len(result.Records) > 0 {
		pruned, _ := result.Records[0].Get("pruned")
		return int(pruned.(int64)), nil
	}

	return 0, nil
}

func (v *VesselGraph) GetShardsByCommunity(ctx context.Context, communityID int64) ([]Shard, error) {
	query := `
	MATCH (s:Shard {community: $communityID})
	WHERE s.category <> 'archived'
	  AND s.category <> 'contract'
	  AND NOT s.id STARTS WITH 'comm-summary-'
	RETURN s
	ORDER BY s.pagerank DESC
	`
	result, err := neo4j.ExecuteQuery(ctx, v.driver, query,
		map[string]any{"communityID": communityID},
		neo4j.EagerResultTransformer,
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

	var lu, ca time.Time
	if luVal, ok := props["last_used"].(string); ok {
		lu, _ = time.Parse(time.RFC3339, luVal)
	} else if luVal, ok := props["last_used"].(time.Time); ok {
		lu = luVal
	}

	if caVal, ok := props["created_at"].(string); ok {
		ca, _ = time.Parse(time.RFC3339, caVal)
	} else if caVal, ok := props["created_at"].(time.Time); ok {
		ca = caVal
	}

	// Zero-timestamp guard: prevent 0001-01-01 from collapsing the Ebbinghaus decay denominator.
	// If created_at is zero, fall back to now. If last_used is zero, fall back to created_at.
	now := time.Now()
	if ca.IsZero() || ca.Year() < 2000 {
		ca = now
	}
	if lu.IsZero() || lu.Year() < 2000 {
		lu = ca
	}

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

	// Cognitive Science Fields (v4.0)
	var salience float64
	if sal, ok := props["salience"].(float64); ok {
		salience = sal
	} else if sal, ok := props["salience"].(int64); ok {
		salience = float64(sal)
	}

	var retrievalHistory []time.Time
	if rh, ok := props["retrieval_history"].([]any); ok {
		retrievalHistory = deserializeRetrievalHistory(rh)
	}

	return Shard{
		ID:               id,
		Category:         cat,
		Content:          cont,
		Metadata:         []byte(meta),
		Vector:           vec,
		SourceType:       srcType,
		SourceRef:        srcRef,
		Confidence:       conf,
		CommunityID:      commID,
		PageRank:         rank,
		LastUsed:         lu,
		CreatedAt:        ca,
		UseCount:         useCount,
		Salience:         salience,
		RetrievalHistory: retrievalHistory,
	}
}

func nodeToShardWithCache(node neo4j.Node) Shard {
	s := nodeToShard(node)
	communityCacheMu.RLock()
	if m, ok := communityCache[s.ID]; ok {
		s.CommunityID = m.CommunityID
		s.PageRank = m.PageRank
	}
	communityCacheMu.RUnlock()
	return s
}

// Ping verifies Neo4j connectivity for health checks.
func (v *VesselGraph) Ping(ctx context.Context) error {
	return v.driver.VerifyConnectivity(ctx)
}

// GetBondCount returns the total number of CONNECTED_TO relationships in the mesh.
func (v *VesselGraph) GetBondCount(ctx context.Context) (int, error) {
	query := "MATCH ()-[r:CONNECTED_TO]->() RETURN count(r) AS count"
	res, err := neo4j.ExecuteQuery(ctx, v.driver, query, nil, neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithDatabase(v.dbName))
	if err != nil {
		return 0, err
	}
	count, _ := res.Records[0].Get("count")
	return int(count.(int64)), nil
}

// GetCommunityCount returns the number of distinct communities assigned by the Synthesizer.
func (v *VesselGraph) GetCommunityCount(ctx context.Context) (int, error) {
	query := "MATCH (s:Shard) WHERE s.community IS NOT NULL RETURN count(DISTINCT s.community) AS count"
	res, err := neo4j.ExecuteQuery(ctx, v.driver, query, nil, neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithDatabase(v.dbName))
	if err != nil {
		return 0, err
	}
	count, _ := res.Records[0].Get("count")
	return int(count.(int64)), nil
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
		slog.Info("shard evicted from mesh", "id", id)
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
		slog.Debug("bond forged", "from", b.FromID, "to", b.ToID, "weight", b.Weight)
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
		shard := nodeToShardWithCache(node.(neo4j.Node))
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
			node.use_count = coalesce(node.use_count, 0) + 1,
			node.retrieval_history = (coalesce(node.retrieval_history, []) + [toString(datetime())])[-20..]
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
	vec := DecodeVector(queryVector)
	
	// Multi-Hop: Find the center AND its connected neighbors + relationships
	var query string
	// topK: find multiple center nodes so different queries produce different results
	topK := limit
	if topK < 3 {
		topK = 3
	}
	if topK > 20 {
		topK = 20
	}

	if shouldTouch {
		query = `
		CALL db.index.vector.queryNodes('shard_embeddings', $topK, $vector)
		YIELD node AS center, score
		SET center.last_used = datetime(),
			center.use_count = coalesce(center.use_count, 0) + 1,
			center.retrieval_history = (coalesce(center.retrieval_history, []) + [toString(datetime())])[-20..]
		WITH center
		OPTIONAL MATCH (center)-[r:CONNECTED_TO]-(neighbor:Shard)
		SET neighbor.last_used = datetime(),
			neighbor.use_count = coalesce(neighbor.use_count, 0) + 1,
			neighbor.retrieval_history = (coalesce(neighbor.retrieval_history, []) + [toString(datetime())])[-20..]
		WITH center, neighbor, r
		OPTIONAL MATCH (neighbor)-[nr:CONNECTED_TO]-()
		WITH center, neighbor, r, count(nr) AS neighborDegree
		RETURN center,
		       count(r) AS centerDegree,
		       collect({node: neighbor, weight: r.weight, degree: neighborDegree}) AS neighbors
		`
	} else {
		query = `
		CALL db.index.vector.queryNodes('shard_embeddings', $topK, $vector)
		YIELD node AS center, score
		WITH center
		OPTIONAL MATCH (center)-[r:CONNECTED_TO]-(neighbor:Shard)
		WITH center, neighbor, r
		OPTIONAL MATCH (neighbor)-[nr:CONNECTED_TO]-()
		WITH center, neighbor, r, count(nr) AS neighborDegree
		RETURN center,
		       count(r) AS centerDegree,
		       collect({node: neighbor, weight: r.weight, degree: neighborDegree}) AS neighbors
		`
	}
	params := map[string]any{
		"vector": vec,
		"topK":   int64(topK),
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
			if deg, ok := m["degree"].(int64); ok {
				neighborShard.BondCount = int(deg)
			}
			
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
	  AND size(s1.embedding) > 0
	  AND size(s2.embedding) > 0
	  AND size(s1.embedding) = size(s2.embedding)
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
	WHERE s.category <> 'core' AND s.category <> 'community'
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

// GetSurvivalDistribution returns bucketed counts of shards by age.
func (v *VesselGraph) GetSurvivalDistribution(ctx context.Context) (map[string]int, error) {
	query := `
	MATCH (s:Shard)
	WHERE s.created_at IS NOT NULL
	WITH s, duration.between(datetime(s.created_at), datetime()).days AS ageDays
	RETURN
		CASE
			WHEN ageDays <= 1 THEN '24h'
			WHEN ageDays <= 7 THEN '7d'
			WHEN ageDays <= 30 THEN '30d'
			WHEN ageDays <= 90 THEN '90d'
			ELSE 'older'
		END AS bucket,
		count(s) AS cnt
	`
	result, err := neo4j.ExecuteQuery(ctx, v.driver, query, nil, neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithDatabase(v.dbName))
	if err != nil {
		return nil, fmt.Errorf("survival distribution query failed: %w", err)
	}

	dist := map[string]int{"24h": 0, "7d": 0, "30d": 0, "90d": 0, "older": 0}
	for _, record := range result.Records {
		bucket, _ := record.Get("bucket")
		cnt, _ := record.Get("cnt")
		dist[bucket.(string)] = int(cnt.(int64))
	}
	return dist, nil
}

// Optimize performs Neo4j maintenance (mostly a no-op as Neo4j handles it)
func (v *VesselGraph) Optimize(ctx context.Context) error {
	// Neo4j handles space reclamation internally.
	// Our Vector index is ensured on startup, but we can call it here just to be safe.
	return v.ensureIndexes(ctx)
}

// --- Retrieval History Serialization ---

// serializeRetrievalHistory converts Go timestamps to RFC3339 strings for Neo4j storage.
func serializeRetrievalHistory(history []time.Time) []string {
	out := make([]string, len(history))
	for i, t := range history {
		out[i] = t.Format(time.RFC3339)
	}
	return out
}

// deserializeRetrievalHistory converts Neo4j string list back to Go timestamps.
func deserializeRetrievalHistory(raw []any) []time.Time {
	out := make([]time.Time, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			if t, err := time.Parse(time.RFC3339, s); err == nil {
				out = append(out, t)
			}
		}
	}
	return out
}

// --- Observation Tools (metadata-only, no touch) ---

func (v *VesselGraph) GetRecentShards(ctx context.Context, limit int, category string) ([]ShardMetadata, error) {
	// Fetch full nodes + bond degree so we can compute SurvivalScoreV4 in Go.
	// Cypher handles ordering and limiting — Go only computes survival for the small result set.
	query := `
	MATCH (s:Shard)
	WHERE s.category <> 'archived'
	  AND ($category = '' OR s.category = $category)
	OPTIONAL MATCH (s)-[r:CONNECTED_TO]-()
	WITH s, count(r) AS degree
	RETURN s, degree
	ORDER BY s.last_used DESC
	LIMIT $limit
	`
	params := map[string]any{"category": category, "limit": int64(limit)}

	result, err := neo4j.ExecuteQuery(ctx, v.driver, query, params, neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithDatabase(v.dbName))
	if err != nil {
		return nil, err
	}

	shards := make([]ShardMetadata, 0, len(result.Records))
	for _, record := range result.Records {
		node, _ := record.Get("s")
		degree, _ := record.Get("degree")
		shard := nodeToShard(node.(neo4j.Node))
		bondCount := int(degree.(int64))
		score := SurvivalScoreV4(bondCount, shard.PageRank, shard.Salience, shard.RetrievalHistory, shard.LastUsed)

		shards = append(shards, ShardMetadata{
			ID:            shard.ID,
			Category:      shard.Category,
			SurvivalScore: score,
			CreatedAt:     shard.CreatedAt,
			LastUsed:      shard.LastUsed,
		})
	}
	return shards, nil
}

func (v *VesselGraph) GetShardsByCategory(ctx context.Context, category string, limit int) ([]ShardMetadata, error) {
	query := `
	MATCH (s:Shard {category: $category})
	WHERE s.category <> 'archived'
	OPTIONAL MATCH (s)-[r:CONNECTED_TO]-()
	WITH s, count(r) AS degree
	RETURN s, degree
	ORDER BY s.last_used DESC
	LIMIT $limit
	`
	params := map[string]any{"category": category, "limit": int64(limit)}

	result, err := neo4j.ExecuteQuery(ctx, v.driver, query, params, neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithDatabase(v.dbName))
	if err != nil {
		return nil, err
	}

	shards := make([]ShardMetadata, 0, len(result.Records))
	for _, record := range result.Records {
		node, _ := record.Get("s")
		degree, _ := record.Get("degree")
		shard := nodeToShard(node.(neo4j.Node))
		bondCount := int(degree.(int64))
		score := SurvivalScoreV4(bondCount, shard.PageRank, shard.Salience, shard.RetrievalHistory, shard.LastUsed)

		shards = append(shards, ShardMetadata{
			ID:            shard.ID,
			Category:      shard.Category,
			SurvivalScore: score,
			CreatedAt:     shard.CreatedAt,
			LastUsed:      shard.LastUsed,
		})
	}
	return shards, nil
}

// GetAtRiskShards fetches non-core shards and filters to those with survival below threshold.
// Survival is computed in Go via SurvivalScoreV4 (same as GetEvictionCandidates).
// CRITICAL: Pure read — no ReinforceShards, no last_used update, no RetrievalHistory append.
func (v *VesselGraph) GetAtRiskShards(ctx context.Context, limit int, threshold float64) ([]ShardMetadata, error) {
	// Fetch all non-core shards with their bond degree for SurvivalScoreV4 input.
	// No Cypher-side filtering on survival — it's computed in Go.
	query := `
	MATCH (s:Shard)
	WHERE s.category <> 'core' AND s.category <> 'community' AND s.category <> 'archived'
	OPTIONAL MATCH (s)-[r:CONNECTED_TO]-()
	RETURN s, count(r) AS degree
	`
	result, err := neo4j.ExecuteQuery(ctx, v.driver, query, nil, neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithDatabase(v.dbName))
	if err != nil {
		return nil, err
	}

	type scored struct {
		meta  ShardMetadata
		score float64
	}
	var candidates []scored

	for _, record := range result.Records {
		node, _ := record.Get("s")
		degree, _ := record.Get("degree")

		shard := nodeToShard(node.(neo4j.Node))
		bondCount := int(degree.(int64))

		score := SurvivalScoreV4(bondCount, shard.PageRank, shard.Salience, shard.RetrievalHistory, shard.LastUsed)
		if score >= threshold {
			continue
		}

		candidates = append(candidates, scored{
			meta: ShardMetadata{
				ID:            shard.ID,
				Category:      shard.Category,
				SurvivalScore: score,
				CreatedAt:     shard.CreatedAt,
				LastUsed:      shard.LastUsed,
			},
			score: score,
		})
	}

	// Sort ascending — lowest survival first
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score < candidates[j].score
	})

	if limit > len(candidates) {
		limit = len(candidates)
	}
	result_meta := make([]ShardMetadata, limit)
	for i := 0; i < limit; i++ {
		result_meta[i] = candidates[i].meta
	}
	return result_meta, nil
}

// --- CRUD ---

func (v *VesselGraph) UpdateShard(ctx context.Context, id string, updates ShardUpdate) error {
	query := `
	MATCH (s:Shard {id: $id})
	SET s.content = CASE WHEN $content <> '' THEN $content ELSE s.content END,
	    s.category = CASE WHEN $category <> '' THEN $category ELSE s.category END,
	    s.embedding = CASE WHEN $hasVector THEN $vector ELSE s.embedding END,
	    s.last_used = datetime()
	RETURN s.id AS id
	`
	// Neo4j stores the vector as the "embedding" property ([]float64 list).
	// Convert []byte → []float64 for Neo4j compatibility.
	var neoVec any
	hasVector := len(updates.Vector) > 0
	if hasVector {
		floats := DecodeVector(updates.Vector)
		f64 := make([]float64, len(floats))
		for i, f := range floats {
			f64[i] = float64(f)
		}
		neoVec = f64
	}

	params := map[string]any{
		"id":        id,
		"content":   updates.Content,
		"category":  updates.Category,
		"hasVector": hasVector,
		"vector":    neoVec,
	}

	result, err := neo4j.ExecuteQuery(ctx, v.driver, query, params, neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithDatabase(v.dbName))
	if err != nil {
		return err
	}
	if len(result.Records) == 0 {
		return fmt.Errorf("shard %s not found in mesh", id)
	}
	return nil
}

func (v *VesselGraph) DeleteShard(ctx context.Context, id string) error {
	// Two-step: verify existence, then delete. DETACH DELETE on a non-existent
	// node silently succeeds, so we check counters to report "not found".
	query := `
	MATCH (s:Shard {id: $id})
	DETACH DELETE s
	`
	result, err := neo4j.ExecuteQuery(ctx, v.driver, query, map[string]any{"id": id}, neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithDatabase(v.dbName))
	if err != nil {
		return err
	}
	if result.Summary.Counters().NodesDeleted() == 0 {
		return fmt.Errorf("shard %s not found in mesh", id)
	}
	return nil
}
