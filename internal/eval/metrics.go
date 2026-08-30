package eval

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Hendrixx-RE/Vektix/internal/format"
	"github.com/Hendrixx-RE/Vektix/internal/router"
)

// IntentCase represents an evaluation case for intent classification.
type IntentCase struct {
	Input    string         `json:"input"`
	Expected *router.Intent `json:"expected"`
	Tier     int            `json:"tier"`
}

// IntentCaseResult holds the evaluation outcome for a single intent case.
type IntentCaseResult struct {
	Case       IntentCase     `json:"case"`
	Actual     *router.Intent `json:"actual,omitempty"`
	ActualTier int            `json:"actual_tier"`
	ActionOK   bool           `json:"action_ok"`
	ParamsOK   bool           `json:"params_ok"`
	TierOK     bool           `json:"tier_ok"`
	Latency    time.Duration  `json:"latency"`
	Error      string         `json:"error,omitempty"`
}

// IntentMetrics summarizes the performance over a suite of intent cases.
type IntentMetrics struct {
	TotalCases       int           `json:"total_cases"`
	ActionMatches    int           `json:"action_matches"`
	ParamMatches     int           `json:"param_matches"`
	Tier1Matches     int           `json:"tier1_matches"`
	Tier1ActualCount int           `json:"tier1_actual_count"`
	Tier2ActualCount int           `json:"tier2_actual_count"`
	ActionAccuracy   float64       `json:"action_accuracy"`
	ParamAccuracy    float64       `json:"param_accuracy"`
	Tier1Rate        float64       `json:"tier1_rate"`
	P50Latency       time.Duration `json:"p50_latency"`
	P95Latency       time.Duration `json:"p95_latency"`
}

func (m IntentMetrics) String() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# intent action accuracy       %4.1f%%\n", m.ActionAccuracy))
	sb.WriteString(fmt.Sprintf("# intent parameter accuracy    %4.1f%%\n", m.ParamAccuracy))
	sb.WriteString(fmt.Sprintf("# tier 1 fastpath rate         %4.1f%%\n", m.Tier1Rate))
	sb.WriteString(fmt.Sprintf("# latency p50 %v   p95 %v\n", format.FormatDuration(m.P50Latency), format.FormatDuration(m.P95Latency)))
	return sb.String()
}

// CheckIntentParams returns true if the actual intent's parameters match the expected intent's parameters.
func CheckIntentParams(expected, actual *router.Intent) bool {
	if expected == nil && actual == nil {
		return true
	}
	if expected == nil || actual == nil {
		return false
	}

	// 1. Path parameter check
	if expected.Path != "" {
		expPath := strings.ToLower(filepath.Clean(filepath.ToSlash(expected.Path)))
		actPath := strings.ToLower(filepath.Clean(filepath.ToSlash(actual.Path)))
		actQuery := strings.ToLower(actual.Query)

		if actPath != expPath && !strings.Contains(actPath, expPath) && !strings.Contains(actQuery, expPath) {
			return false
		}
	}

	// 2. Query parameter check
	if expected.Query != "" {
		expQuery := strings.ToLower(strings.TrimSpace(expected.Query))
		actQuery := strings.ToLower(strings.TrimSpace(actual.Query))
		actPath := strings.ToLower(strings.TrimSpace(actual.Path))

		if !strings.Contains(actQuery, expQuery) && !strings.Contains(actPath, expQuery) && !strings.Contains(expQuery, actQuery) {
			return false
		}
	}

	// 3. Lines parameter check
	if expected.Lines != "" {
		if strings.TrimSpace(actual.Lines) != strings.TrimSpace(expected.Lines) {
			return false
		}
	}

	return true
}

// ComputeIntentMetrics aggregates single-case intent results into a summary.
func ComputeIntentMetrics(results []IntentCaseResult) IntentMetrics {
	m := IntentMetrics{TotalCases: len(results)}
	if len(results) == 0 {
		return m
	}

	latencies := make([]time.Duration, 0, len(results))
	for _, r := range results {
		latencies = append(latencies, r.Latency)
		if r.ActionOK {
			m.ActionMatches++
		}
		if r.ParamsOK {
			m.ParamMatches++
		}
		if r.TierOK {
			m.Tier1Matches++
		}
		if r.ActualTier == 1 {
			m.Tier1ActualCount++
		} else {
			m.Tier2ActualCount++
		}
	}

	m.ActionAccuracy = float64(m.ActionMatches) / float64(m.TotalCases) * 100.0
	m.ParamAccuracy = float64(m.ParamMatches) / float64(m.TotalCases) * 100.0
	m.Tier1Rate = float64(m.Tier1ActualCount) / float64(m.TotalCases) * 100.0
	m.P50Latency = ComputePercentile(latencies, 0.50)
	m.P95Latency = ComputePercentile(latencies, 0.95)

	return m
}

// LocateCase represents an evaluation case for locate retrieval.
type LocateCase struct {
	Query        string `json:"query"`
	ExpectPath   string `json:"expect_path"`
	Scope        string `json:"scope"`
	ExpectedText string `json:"expected_text,omitempty"`
}

// LocateCaseResult holds the evaluation outcome for a single locate case.
type LocateCaseResult struct {
	Case           LocateCase    `json:"case"`
	GlobalHitAt1   bool          `json:"global_hit_at_1"`
	GlobalHitAt3   bool          `json:"global_hit_at_3"`
	ScopedHitAt1   bool          `json:"scoped_hit_at_1"`
	ScopedHitAt3   bool          `json:"scoped_hit_at_3"`
	BM25HitAt3     bool          `json:"bm25_hit_at_3"`
	VectorHitAt3   bool          `json:"vector_hit_at_3"`
	RRFHitAt3      bool          `json:"rrf_hit_at_3"`
	ExcerptOK      bool          `json:"excerpt_ok"`
	Latency        time.Duration `json:"latency"`
	GlobalTopPaths []string      `json:"global_top_paths"`
	ScopedTopPaths []string      `json:"scoped_top_paths"`
	Warnings       []string      `json:"warnings,omitempty"`
}

// LocateMetrics summarizes the performance over a suite of locate cases.
type LocateMetrics struct {
	TotalCases      int           `json:"total_cases"`
	LocateRecallAt1 float64       `json:"locate_recall_at_1"`
	LocateRecallAt3 float64       `json:"locate_recall_at_3"`
	ScopedRecallAt1 float64       `json:"scoped_recall_at_1"`
	ScopedRecallAt3 float64       `json:"scoped_recall_at_3"`
	ScopedDeltaAt1  float64       `json:"scoped_delta_at_1"`
	ScopedDeltaAt3  float64       `json:"scoped_delta_at_3"`
	BM25Recall      float64       `json:"bm25_recall"`
	VectorRecall    float64       `json:"vector_recall"`
	RRFRecall       float64       `json:"rrf_recall"`
	ExcerptAccuracy float64       `json:"excerpt_accuracy"`
	HasExcerptCases bool          `json:"has_excerpt_cases"`
	P50Latency      time.Duration `json:"p50_latency"`
	P95Latency      time.Duration `json:"p95_latency"`
}

func (m LocateMetrics) String() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# locate recall@1   %4.1f%%   locate recall@3   %4.1f%%\n", m.LocateRecallAt1, m.LocateRecallAt3))
	sb.WriteString(fmt.Sprintf("# scoped recall@1   %4.1f%%   scoped recall@3   %4.1f%%   (%+.1f / %+.1f)\n",
		m.ScopedRecallAt1, m.ScopedRecallAt3, m.ScopedDeltaAt1, m.ScopedDeltaAt3))
	sb.WriteString(fmt.Sprintf("# ablation: bm25 %4.1f  vector %4.1f  rrf %4.1f\n", m.BM25Recall, m.VectorRecall, m.RRFRecall))
	sb.WriteString(fmt.Sprintf("# latency p50 %v   p95 %v\n", format.FormatDuration(m.P50Latency), format.FormatDuration(m.P95Latency)))
	if m.HasExcerptCases {
		sb.WriteString(fmt.Sprintf("# excerpt correctness  %4.1f%%\n", m.ExcerptAccuracy))
	}
	return sb.String()
}

// CheckPathMatch returns true if actualPath matches expectPath.
// Supports relative suffixes, exact matches, tilde expansion, and base file name matches.
func CheckPathMatch(actualPath, expectPath string) bool {
	if actualPath == "" || expectPath == "" {
		return false
	}

	act := filepath.Clean(filepath.ToSlash(actualPath))
	exp := filepath.Clean(filepath.ToSlash(expectPath))

	if strings.HasPrefix(exp, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			exp = filepath.Clean(filepath.ToSlash(filepath.Join(home, exp[2:])))
		}
	}

	if act == exp {
		return true
	}

	if strings.HasSuffix(act, "/"+exp) || strings.HasSuffix(act, exp) {
		return true
	}

	if !strings.Contains(expectPath, "/") && filepath.Base(act) == exp {
		return true
	}

	return false
}

// CheckExcerptCorrectness performs a binary check whether content contains expectedText.
func CheckExcerptCorrectness(content, expectedText string) bool {
	if expectedText == "" {
		return true
	}
	return strings.Contains(content, expectedText)
}

// ComputeLocateMetrics aggregates single-case locate results into a summary.
func ComputeLocateMetrics(results []LocateCaseResult) LocateMetrics {
	m := LocateMetrics{TotalCases: len(results)}
	if len(results) == 0 {
		return m
	}

	var (
		g1, g3       int
		s1, s3       int
		bm25, vec    int
		rrf          int
		excerptCases int
		excerptHits  int
		latencies    = make([]time.Duration, 0, len(results))
	)

	for _, r := range results {
		latencies = append(latencies, r.Latency)
		if r.GlobalHitAt1 {
			g1++
		}
		if r.GlobalHitAt3 {
			g3++
		}
		if r.ScopedHitAt1 {
			s1++
		}
		if r.ScopedHitAt3 {
			s3++
		}
		if r.BM25HitAt3 {
			bm25++
		}
		if r.VectorHitAt3 {
			vec++
		}
		if r.RRFHitAt3 {
			rrf++
		}
		if r.Case.ExpectedText != "" {
			excerptCases++
			if r.ExcerptOK {
				excerptHits++
			}
		}
	}

	total := float64(m.TotalCases)
	m.LocateRecallAt1 = float64(g1) / total * 100.0
	m.LocateRecallAt3 = float64(g3) / total * 100.0
	m.ScopedRecallAt1 = float64(s1) / total * 100.0
	m.ScopedRecallAt3 = float64(s3) / total * 100.0
	m.ScopedDeltaAt1 = m.ScopedRecallAt1 - m.LocateRecallAt1
	m.ScopedDeltaAt3 = m.ScopedRecallAt3 - m.LocateRecallAt3
	m.BM25Recall = float64(bm25) / total * 100.0
	m.VectorRecall = float64(vec) / total * 100.0
	m.RRFRecall = float64(rrf) / total * 100.0

	if excerptCases > 0 {
		m.HasExcerptCases = true
		m.ExcerptAccuracy = float64(excerptHits) / float64(excerptCases) * 100.0
	}

	m.P50Latency = ComputePercentile(latencies, 0.50)
	m.P95Latency = ComputePercentile(latencies, 0.95)

	return m
}

// ComputePercentile calculates the p-th percentile (0.0 to 1.0) of durations.
func ComputePercentile(durations []time.Duration, p float64) time.Duration {
	if len(durations) == 0 {
		return 0
	}
	sorted := make([]time.Duration, len(durations))
	copy(sorted, durations)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i] < sorted[j]
	})

	if p <= 0 {
		return sorted[0]
	}
	if p >= 1.0 {
		return sorted[len(sorted)-1]
	}

	idx := int(math.Ceil(p*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}
