package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func metricValue(t *testing.T, familyName string, labels map[string]string) (float64, bool) {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	for _, mf := range families {
		if mf.GetName() != familyName {
			continue
		}
		for _, m := range mf.GetMetric() {
			if len(labels) > 0 {
				matched := 0
				for _, lp := range m.GetLabel() {
					if want, ok := labels[lp.GetName()]; ok && lp.GetValue() == want {
						matched++
					}
				}
				if matched != len(labels) {
					continue
				}
			}
			if g := m.GetGauge(); g != nil {
				return g.GetValue(), true
			}
			if c := m.GetCounter(); c != nil {
				return c.GetValue(), true
			}
		}
	}
	return 0, false
}

func TestMetricHelpers(t *testing.T) {
	RecordBuildInfo("1.2.3", "abc123", "2026-01-01")
	if got, ok := metricValue(t, "pganalyzer_build_info", map[string]string{
		"version": "1.2.3", "commit": "abc123", "build_date": "2026-01-01",
	}); !ok || got != 1 {
		t.Errorf("BuildInfo = %v (found=%v), want 1", got, ok)
	}

	beforeOK, _ := metricValue(t, "pganalyzer_collection_total", map[string]string{"collector": "test_collector", "status": "success"})
	beforeErr, _ := metricValue(t, "pganalyzer_collection_total", map[string]string{"collector": "test_collector", "status": "error"})
	RecordCollectionDuration("test_collector", 0.25, true)
	RecordCollectionDuration("test_collector", 0.50, false)
	if got, ok := metricValue(t, "pganalyzer_collection_total", map[string]string{"collector": "test_collector", "status": "success"}); !ok || got != beforeOK+1 {
		t.Errorf("CollectionTotal(success) = %v, want %v", got, beforeOK+1)
	}
	if got, ok := metricValue(t, "pganalyzer_collection_total", map[string]string{"collector": "test_collector", "status": "error"}); !ok || got != beforeErr+1 {
		t.Errorf("CollectionTotal(error) = %v, want %v", got, beforeErr+1)
	}

	beforeSnaps, _ := metricValue(t, "pganalyzer_snapshots_total", nil)
	beforeSnapErrs, _ := metricValue(t, "pganalyzer_snapshots_errors_total", nil)
	RecordSnapshot(true)
	RecordSnapshot(false)
	if got, ok := metricValue(t, "pganalyzer_snapshots_total", nil); !ok || got != beforeSnaps+2 {
		t.Errorf("SnapshotsTotal = %v, want %v", got, beforeSnaps+2)
	}
	if got, ok := metricValue(t, "pganalyzer_snapshots_errors_total", nil); !ok || got != beforeSnapErrs+1 {
		t.Errorf("SnapshotsErrors = %v, want %v", got, beforeSnapErrs+1)
	}

	beforeAnalysisOK, _ := metricValue(t, "pganalyzer_analysis_total", map[string]string{"status": "success"})
	RecordAnalysis(1.5, true, map[string]int{"critical": 2, "warning": 5})
	if got, ok := metricValue(t, "pganalyzer_analysis_total", map[string]string{"status": "success"}); !ok || got != beforeAnalysisOK+1 {
		t.Errorf("AnalysisTotal(success) = %v, want %v", got, beforeAnalysisOK+1)
	}
	if got, ok := metricValue(t, "pganalyzer_analysis_issues_detected", map[string]string{"severity": "critical"}); !ok || got != 2 {
		t.Errorf("IssuesDetected(critical) = %v, want 2", got)
	}
	if got, ok := metricValue(t, "pganalyzer_analysis_issues_detected", map[string]string{"severity": "warning"}); !ok || got != 5 {
		t.Errorf("IssuesDetected(warning) = %v, want 5", got)
	}

	UpdateDatabaseMetrics(0.995, 120, 4)
	if got, ok := metricValue(t, "pganalyzer_database_cache_hit_ratio", nil); !ok || got != 0.995 {
		t.Errorf("CacheHitRatio = %v, want 0.995", got)
	}
	if got, ok := metricValue(t, "pganalyzer_database_query_count", nil); !ok || got != 120 {
		t.Errorf("QueryCount = %v, want 120", got)
	}
	if got, ok := metricValue(t, "pganalyzer_database_slow_query_count", nil); !ok || got != 4 {
		t.Errorf("SlowQueryCount = %v, want 4", got)
	}

	UpdateSuggestionMetrics(map[string]map[string]int{
		"critical": {"active": 3},
		"warning":  {"active": 7},
	})
	if got, ok := metricValue(t, "pganalyzer_suggestions_total", map[string]string{"severity": "critical", "status": "active"}); !ok || got != 3 {
		t.Errorf("SuggestionsTotal(critical, active) = %v, want 3", got)
	}
	if got, ok := metricValue(t, "pganalyzer_suggestions_total", map[string]string{"severity": "warning", "status": "active"}); !ok || got != 7 {
		t.Errorf("SuggestionsTotal(warning, active) = %v, want 7", got)
	}
	// Calling UpdateSuggestionMetrics again resets stale label values.
	UpdateSuggestionMetrics(map[string]map[string]int{
		"warning": {"active": 1},
	})
	if _, ok := metricValue(t, "pganalyzer_suggestions_total", map[string]string{"severity": "critical", "status": "active"}); ok {
		t.Errorf("expected stale SuggestionsTotal(critical, active) to be removed after Reset")
	}
	if got, ok := metricValue(t, "pganalyzer_suggestions_total", map[string]string{"severity": "warning", "status": "active"}); !ok || got != 1 {
		t.Errorf("SuggestionsTotal(warning, active) = %v, want 1", got)
	}
}
