package synthesizer

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/izenberk/shard-link/internal/metrics"
	"github.com/izenberk/shard-link/internal/storage"
)

// Synthesizer handles autonomous relational linking and community summarization.
type Synthesizer struct {
	vessel         storage.Repository
	embedder       storage.Embedder
	summarizer     storage.Summarizer
	interval       time.Duration
	threshold      float64
	minDirtyShards int64         // Dynamic gate: min shards before synthesis fires
	maxDeferral    time.Duration // Dynamic gate: max time before forced synthesis
	logger         storage.LogFunc
}

func NewSynthesizer(v storage.Repository, interval time.Duration, emb storage.Embedder, sum storage.Summarizer, logger storage.LogFunc) *Synthesizer {
	tStr := os.Getenv("MESH_LINK_THRESHOLD")
	threshold, err := strconv.ParseFloat(tStr, 64)
	if err != nil {
		threshold = 0.75
	}

	minDirty := int64(5)
	if v := os.Getenv("SYNTHESIZER_MIN_DIRTY_SHARDS"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			minDirty = n
		}
	}

	maxDefer := 24 * time.Hour
	if v := os.Getenv("SYNTHESIZER_MAX_DEFERRAL_HOURS"); v != "" {
		if h, err := strconv.Atoi(v); err == nil && h > 0 {
			maxDefer = time.Duration(h) * time.Hour
		}
	}

	return &Synthesizer{
		vessel:         v,
		embedder:       emb,
		summarizer:     sum,
		interval:       interval,
		threshold:      threshold,
		minDirtyShards: minDirty,
		maxDeferral:    maxDefer,
		logger:         logger,
	}
}

func (s *Synthesizer) logActivity(msg, category, shardID string) {
	if s.logger != nil {
		s.logger(msg, category, shardID)
	}
}

func (s *Synthesizer) Run(ctx context.Context) {
	slog.Info("synthesizer started", "interval", s.interval, "threshold", s.threshold)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("synthesizer shutting down")
			return
		case <-ticker.C:
			s.performSynthesis(ctx)
		}
	}
}

func (s *Synthesizer) performSynthesis(ctx context.Context) {
	dirty := storage.DirtyShardCount()
	if dirty == 0 {
		slog.Debug("synthesizer: mesh idle — skipping cycle")
		return
	}

	// Dynamic gate: synthesize only when enough shards accumulated OR the
	// deferral ceiling has been exceeded.
	deferred := time.Since(storage.LastSynthesisTime())
	if dirty < s.minDirtyShards && deferred < s.maxDeferral {
		slog.Debug("synthesizer: deferring",
			"dirty", dirty, "threshold", s.minDirtyShards,
			"deferred", deferred.Round(time.Second))
		return
	}

	reason := "threshold reached"
	if dirty < s.minDirtyShards {
		reason = "max deferral exceeded"
	}
	slog.Info("synthesizer: gate passed", "reason", reason,
		"dirty", dirty, "threshold", s.minDirtyShards)

	count, err := s.vessel.SyncBonds(ctx, s.threshold)
	storage.ConsumeDirtyShards(dirty) // replaces ClearMeshDirty(), same position
	if err != nil {
		slog.Error("synthesizer bond sync failed", "error", err)
		return
	}

	if count > 0 {
		metrics.SynthesizerBondsCreatedTotal.Add(int64(count))
		slog.Info("synthesizer forged new bonds", "count", count)
		s.logActivity(fmt.Sprintf("Synthesizer: %d new semantic bonds forged autonomously", count), "bond", "")

		// After linking, trigger community refresh asynchronously to avoid blocking.
		// Derives from parent ctx so it stops on shutdown — not context.Background().
		go func() {
			slog.Info("synthesizer refreshing communities")
			s.logActivity("Synthesizer: refreshing Knowledge Neighborhoods...", "info", "")
			bgCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			defer cancel()

			_, changedCommunities, err := s.vessel.CalculateCommunities(bgCtx)
			if err != nil {
				slog.Error("synthesizer community calculation failed", "error", err)
				s.logActivity(fmt.Sprintf("Synthesizer: community calculation failed — %v", err), "error", "")
				return
			}

			pruned, err := s.vessel.PruneStaleSummaries(bgCtx)
			if err != nil {
				slog.Error("synthesizer failed to prune stale summaries", "error", err)
			} else if pruned > 0 {
				slog.Info("synthesizer pruned stale summaries", "pruned", pruned)
				s.logActivity(fmt.Sprintf("Synthesizer: pruned %d stale community summaries", pruned), "system", "")
			}

			if len(changedCommunities) > 0 && s.summarizer != nil && s.embedder != nil {
				s.summarizeCommunities(bgCtx, changedCommunities)
			}
		}()
	} else {
		slog.Debug("synthesizer: no new relationships this cycle")
		s.logActivity("Synthesizer: no new relationships in this cycle", "system", "")
	}
}

func (s *Synthesizer) summarizeCommunities(parentCtx context.Context, communityIDs []int64) {
	// Separate timeout — summarization is LLM-bound and should not be tied
	// to the 2-minute community calculation timeout. Derives from parent so
	// it respects shutdown cancellation.
	ctx, cancel := context.WithTimeout(parentCtx, 5*time.Minute)
	defer cancel()

	slog.Info("synthesizer summarizing communities", "count", len(communityIDs))

	for i, cid := range communityIDs {
		members, err := s.vessel.GetShardsByCommunity(ctx, cid)
		if err != nil {
			slog.Error("synthesizer failed to fetch community members",
				"community_id", cid, "error", err)
			s.logActivity(fmt.Sprintf("Synthesizer: failed to fetch community %d members — %v", cid, err), "error", "")
			continue
		}

		if len(members) < 3 {
			slog.Debug("synthesizer skipping small community",
				"community_id", cid, "members", len(members))
			continue
		}

		prompt := buildSummaryPrompt(cid, members)

		summary, err := s.summarizer.Summarize(ctx, prompt)
		if err != nil {
			slog.Error("synthesizer summarization failed",
				"community_id", cid, "error", err)
			s.logActivity(fmt.Sprintf("Synthesizer: failed to summarize community %d — %v", cid, err), "error", "")
			continue
		}

		vec, err := s.embedder.Embed(ctx, summary)
		if err != nil {
			slog.Error("synthesizer embedding failed",
				"community_id", cid, "error", err)
			s.logActivity(fmt.Sprintf("Synthesizer: failed to embed community %d summary — %v", cid, err), "error", "")
			continue
		}

		shardID := fmt.Sprintf("comm-summary-%d", cid)
		shard := storage.Shard{
			ID:       shardID,
			Category: "core",
			Content:  summary,
			Vector:   storage.EncodeVector(vec),
			Metadata: []byte(fmt.Sprintf(`{"community_id":%d,"member_count":%d}`, cid, len(members))),
		}

		if err := s.vessel.SaveShard(ctx, shard); err != nil {
			slog.Error("synthesizer failed to save community summary",
				"shard_id", shardID, "community_id", cid, "error", err)
			s.logActivity(fmt.Sprintf("Synthesizer: failed to save community %d summary — %v", cid, err), "error", shardID)
			continue
		}

		metrics.SynthesizerSummariesTotal.Add(1)
		slog.Info("synthesizer community summarized",
			"community_id", cid, "shard_id", shardID, "members", len(members))
		s.logActivity(fmt.Sprintf("Community %d summarized -> %s (%d members)", cid, shardID, len(members)), "success", shardID)

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
