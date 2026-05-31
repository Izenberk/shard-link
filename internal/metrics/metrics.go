package metrics

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// --- Counters (monotonically increasing) ---

// Search request counters — one per MCP tool.
var (
	SearchAllTotal    atomic.Int64
	SearchMemoryTotal atomic.Int64
	SearchTextTotal   atomic.Int64
	SearchGraphTotal  atomic.Int64
)

// Cognitive engine counters.
var (
	JanitorEvictionsTotal        atomic.Int64
	SynthesizerBondsCreatedTotal atomic.Int64
	SynthesizerSummariesTotal    atomic.Int64
)

// Save counter.
var SaveMemoryTotal atomic.Int64

// --- Gauges (point-in-time values, set externally) ---

// MeshGauges holds the latest mesh state snapshot.
// Updated by Visual Ego on each /api/graph or /api/health call.
type MeshGauges struct {
	ShardsCore     int
	ShardsMemory   int
	ShardsSession  int
	ShardsArchived int
	BondsTotal     int
	CommunitiesMax int64 // Highest community ID observed
}

var (
	meshGauges   MeshGauges
	meshGaugesMu sync.RWMutex
)

func SetMeshGauges(g MeshGauges) {
	meshGaugesMu.Lock()
	meshGauges = g
	meshGaugesMu.Unlock()
}

func GetMeshGauges() MeshGauges {
	meshGaugesMu.RLock()
	defer meshGaugesMu.RUnlock()
	return meshGauges
}

// --- Latency tracking (histogram-style, bucketed) ---

// LatencyTracker records durations into fixed buckets for a named operation.
// Thread-safe via atomic operations on each bucket counter.
type LatencyTracker struct {
	name    string
	buckets []bucket
	sum     atomic.Int64 // Microseconds total
	count   atomic.Int64
}

type bucket struct {
	le    float64 // Upper bound in seconds
	count atomic.Int64
}

// NewLatencyTracker creates a tracker with standard Prometheus-style buckets.
// Buckets are chosen for sub-second operations: 1ms, 5ms, 10ms, 50ms, 100ms, 500ms, 1s, 5s.
func NewLatencyTracker(name string) *LatencyTracker {
	bounds := []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1.0, 5.0}
	lt := &LatencyTracker{name: name}
	for _, b := range bounds {
		lt.buckets = append(lt.buckets, bucket{le: b})
	}
	return lt
}

// Observe records a single duration observation.
func (lt *LatencyTracker) Observe(d time.Duration) {
	sec := d.Seconds()
	lt.sum.Add(int64(d.Microseconds()))
	lt.count.Add(1)
	for i := range lt.buckets {
		if sec <= lt.buckets[i].le {
			lt.buckets[i].count.Add(1)
		}
	}
}

// Pre-built latency trackers for search operations.
var (
	SearchAllLatency    = NewLatencyTracker("shard_link_search_duration_seconds")
	SearchMemoryLatency = NewLatencyTracker("shard_link_search_memory_duration_seconds")
	SearchTextLatency   = NewLatencyTracker("shard_link_search_text_duration_seconds")
	SearchGraphLatency  = NewLatencyTracker("shard_link_search_graph_duration_seconds")
	JanitorCycleLatency = NewLatencyTracker("shard_link_janitor_cycle_duration_seconds")
)

// --- Survival Score Distribution ---

// SurvivalBuckets holds the latest survival score distribution.
// Bucket boundaries: ≤20, ≤50, ≤80, ≤95, ≤100.
type SurvivalBuckets struct {
	Le20  int `json:"le_20"`
	Le50  int `json:"le_50"`
	Le80  int `json:"le_80"`
	Le95  int `json:"le_95"`
	Le100 int `json:"le_100"`
	Total int `json:"total"`
}

var (
	survivalBuckets   SurvivalBuckets
	survivalBucketsMu sync.RWMutex
)

func SetSurvivalBuckets(b SurvivalBuckets) {
	survivalBucketsMu.Lock()
	survivalBuckets = b
	survivalBucketsMu.Unlock()
}

func GetSurvivalBuckets() SurvivalBuckets {
	survivalBucketsMu.RLock()
	defer survivalBucketsMu.RUnlock()
	return survivalBuckets
}

// --- Prometheus Text Format Renderer ---

// RenderPrometheus returns all metrics in Prometheus exposition format.
// No external dependencies — just formatted strings.
func RenderPrometheus() string {
	var b strings.Builder

	// Mesh state gauges
	g := GetMeshGauges()
	writeGauge(&b, "shard_link_shards_total", "Total shards by category",
		label{"category", "core"}, float64(g.ShardsCore))
	writeGaugeLine(&b, "shard_link_shards_total",
		label{"category", "memory"}, float64(g.ShardsMemory))
	writeGaugeLine(&b, "shard_link_shards_total",
		label{"category", "session"}, float64(g.ShardsSession))
	writeGaugeLine(&b, "shard_link_shards_total",
		label{"category", "archived"}, float64(g.ShardsArchived))

	writeGauge(&b, "shard_link_bonds_total", "Total semantic bonds in the mesh",
		label{}, float64(g.BondsTotal))
	writeGauge(&b, "shard_link_communities_total", "Number of detected communities",
		label{}, float64(g.CommunitiesMax))

	// Search request counters
	writeCounter(&b, "shard_link_search_requests_total", "Total search requests by tool",
		label{"tool", "search_all"}, float64(SearchAllTotal.Load()))
	writeCounterLine(&b, "shard_link_search_requests_total",
		label{"tool", "search_memory"}, float64(SearchMemoryTotal.Load()))
	writeCounterLine(&b, "shard_link_search_requests_total",
		label{"tool", "search_text"}, float64(SearchTextTotal.Load()))
	writeCounterLine(&b, "shard_link_search_requests_total",
		label{"tool", "search_graph"}, float64(SearchGraphTotal.Load()))

	writeCounter(&b, "shard_link_save_requests_total", "Total save_memory requests",
		label{}, float64(SaveMemoryTotal.Load()))

	// Cognitive engine counters
	writeCounter(&b, "shard_link_janitor_evictions_total", "Total shards evicted by the Janitor",
		label{}, float64(JanitorEvictionsTotal.Load()))
	writeCounter(&b, "shard_link_synthesizer_bonds_created_total", "Total bonds forged by the Synthesizer",
		label{}, float64(SynthesizerBondsCreatedTotal.Load()))
	writeCounter(&b, "shard_link_synthesizer_summaries_generated_total", "Total community summaries generated",
		label{}, float64(SynthesizerSummariesTotal.Load()))

	// Search latency histograms
	writeHistogram(&b, SearchAllLatency)
	writeHistogram(&b, SearchMemoryLatency)
	writeHistogram(&b, SearchTextLatency)
	writeHistogram(&b, SearchGraphLatency)
	writeHistogram(&b, JanitorCycleLatency)

	// Survival score distribution
	sb := GetSurvivalBuckets()
	b.WriteString("# HELP shard_link_survival_score_bucket Survival score distribution across the mesh\n")
	b.WriteString("# TYPE shard_link_survival_score_bucket histogram\n")
	fmt.Fprintf(&b, "shard_link_survival_score_bucket{le=\"20\"} %d\n", sb.Le20)
	fmt.Fprintf(&b, "shard_link_survival_score_bucket{le=\"50\"} %d\n", sb.Le50)
	fmt.Fprintf(&b, "shard_link_survival_score_bucket{le=\"80\"} %d\n", sb.Le80)
	fmt.Fprintf(&b, "shard_link_survival_score_bucket{le=\"95\"} %d\n", sb.Le95)
	fmt.Fprintf(&b, "shard_link_survival_score_bucket{le=\"100\"} %d\n", sb.Le100)
	fmt.Fprintf(&b, "shard_link_survival_score_bucket{le=\"+Inf\"} %d\n", sb.Total)
	fmt.Fprintf(&b, "shard_link_survival_score_count %d\n", sb.Total)

	return b.String()
}

// --- internal helpers ---

type label struct {
	key, value string
}

func writeGauge(b *strings.Builder, name, help string, l label, val float64) {
	fmt.Fprintf(b, "# HELP %s %s\n# TYPE %s gauge\n", name, help, name)
	writeGaugeLine(b, name, l, val)
}

func writeGaugeLine(b *strings.Builder, name string, l label, val float64) {
	if l.key != "" {
		fmt.Fprintf(b, "%s{%s=%q} %g\n", name, l.key, l.value, val)
	} else {
		fmt.Fprintf(b, "%s %g\n", name, val)
	}
}

func writeCounter(b *strings.Builder, name, help string, l label, val float64) {
	fmt.Fprintf(b, "# HELP %s %s\n# TYPE %s counter\n", name, help, name)
	writeCounterLine(b, name, l, val)
}

func writeCounterLine(b *strings.Builder, name string, l label, val float64) {
	if l.key != "" {
		fmt.Fprintf(b, "%s{%s=%q} %g\n", name, l.key, l.value, val)
	} else {
		fmt.Fprintf(b, "%s %g\n", name, val)
	}
}

func writeHistogram(b *strings.Builder, lt *LatencyTracker) {
	fmt.Fprintf(b, "# HELP %s Duration in seconds\n# TYPE %s histogram\n", lt.name, lt.name)
	for i := range lt.buckets {
		fmt.Fprintf(b, "%s_bucket{le=%q} %d\n", lt.name, fmt.Sprintf("%g", lt.buckets[i].le), lt.buckets[i].count.Load())
	}
	fmt.Fprintf(b, "%s_bucket{le=\"+Inf\"} %d\n", lt.name, lt.count.Load())
	fmt.Fprintf(b, "%s_sum %g\n", lt.name, float64(lt.sum.Load())/1e6)
	fmt.Fprintf(b, "%s_count %d\n", lt.name, lt.count.Load())
}
