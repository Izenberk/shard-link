package synthesizer

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/izenberk/shard-link/internal/storage"
)

// Synthesizer handles autonomous relational linking and community summarization.
type Synthesizer struct {
	vessel     storage.Repository
	embedder   storage.Embedder
	summarizer storage.Summarizer
	interval   time.Duration
	threshold  float64
}

func NewSynthesizer(v storage.Repository, interval time.Duration, emb storage.Embedder, sum storage.Summarizer) *Synthesizer {
	tStr := os.Getenv("MESH_LINK_THRESHOLD")
	threshold, err := strconv.ParseFloat(tStr, 64)
	if err != nil {
		threshold = 0.75
	}

	return &Synthesizer{
		vessel:     v,
		embedder:   emb,
		summarizer: sum,
		interval:   interval,
		threshold:  threshold,
	}
}

func (s *Synthesizer) Run(ctx context.Context) {
	log.Printf("[Synthesizer] Service ignited. Interval: %v, Threshold: %.2f", s.interval, s.threshold)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("[Synthesizer] Service shutting down...")
			return
		case <-ticker.C:
			s.performSynthesis(ctx)
		}
	}
}

func (s *Synthesizer) performSynthesis(ctx context.Context) {
	log.Println("[Synthesizer] Analyzing Knowledge Mesh for new semantic bonds...")

	count, err := s.vessel.SyncBonds(ctx, s.threshold)
	if err != nil {
		log.Printf("[Synthesizer ERROR] Failed to sync bonds: %v", err)
		return
	}

	if count > 0 {
		log.Printf("[Synthesizer] Aha! Established %d new semantic bonds autonomously.", count)

		// After linking, trigger community refresh asynchronously to avoid blocking
		go func() {
			log.Println("[Synthesizer] Refreshing Knowledge Neighborhoods (Louvain)...")
			bgCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()

			_, changedCommunities, err := s.vessel.CalculateCommunities(bgCtx)
			if err != nil {
				log.Printf("[Synthesizer ERROR] Failed to calculate communities: %v", err)
				return
			}

			if len(changedCommunities) > 0 && s.summarizer != nil && s.embedder != nil {
				s.summarizeCommunities(changedCommunities)
			}
		}()
	} else {
		log.Println("[Synthesizer] No new relationships identified in this cycle.")
	}
}

func (s *Synthesizer) summarizeCommunities(communityIDs []int64) {
	// Separate timeout — summarization is LLM-bound and should not be tied
	// to the 2-minute community calculation timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	log.Printf("[Synthesizer] Summarizing %d changed communities...", len(communityIDs))

	for i, cid := range communityIDs {
		members, err := s.vessel.GetShardsByCommunity(ctx, cid)
		if err != nil {
			log.Printf("[Synthesizer ERROR] Failed to fetch community %d members: %v", cid, err)
			continue
		}

		// Skip communities with fewer than 2 members — no meaningful summary
		if len(members) < 2 {
			continue
		}

		// Build the LLM prompt from top 15 members (already sorted by PageRank DESC)
		prompt := buildSummaryPrompt(cid, members)

		summary, err := s.summarizer.Summarize(ctx, prompt)
		if err != nil {
			log.Printf("[Synthesizer ERROR] Failed to summarize community %d: %v", cid, err)
			continue
		}

		// Embed the summary for vector retrieval
		vec, err := s.embedder.Embed(ctx, summary)
		if err != nil {
			log.Printf("[Synthesizer ERROR] Failed to embed community %d summary: %v", cid, err)
			continue
		}

		// Save as a core shard with deterministic ID (MERGE upsert)
		shardID := fmt.Sprintf("comm-summary-%d", cid)
		shard := storage.Shard{
			ID:       shardID,
			Category: "core",
			Content:  summary,
			Vector:   storage.EncodeVector(vec),
			Metadata: []byte(fmt.Sprintf(`{"community_id":%d,"member_count":%d}`, cid, len(members))),
		}

		if err := s.vessel.SaveShard(ctx, shard); err != nil {
			log.Printf("[Synthesizer ERROR] Failed to save community %d summary shard: %v", cid, err)
			continue
		}

		log.Printf("[Synthesizer] Community %d summarized and saved as %s (%d members)", cid, shardID, len(members))

		// Rate limit: 2-second delay between Gemini API calls (free tier: 15 RPM)
		if i < len(communityIDs)-1 {
			time.Sleep(2 * time.Second)
		}
	}
}

func buildSummaryPrompt(communityID int64, members []storage.Shard) string {
	var b strings.Builder
	b.WriteString("You are a knowledge graph analyst. Summarize the following cluster of related knowledge fragments into a single cohesive paragraph. ")
	b.WriteString("Focus on the overarching theme, key topics, and how they connect. Be concise but informative.\n\n")
	b.WriteString(fmt.Sprintf("Community ID: %d\n", communityID))
	b.WriteString(fmt.Sprintf("Member count: %d\n\n", len(members)))
	b.WriteString("Fragments (ordered by centrality):\n\n")

	charBudget := 12000
	used := 0
	limit := 15
	if len(members) < limit {
		limit = len(members)
	}

	for i := 0; i < limit; i++ {
		entry := fmt.Sprintf("--- [%s] (category: %s) ---\n%s\n\n", members[i].ID, members[i].Category, members[i].Content)
		if used+len(entry) > charBudget {
			break
		}
		b.WriteString(entry)
		used += len(entry)
	}

	return b.String()
}
