// Package metrics provides Prometheus metrics for pganalyzer.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const namespace = "pganalyzer"

var (
	// Collection metrics
	CollectionDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "collection",
			Name:      "duration_seconds",
			Help:      "Duration of data collection in seconds",
			Buckets:   []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60},
		},
		[]string{"collector"},
	)

	CollectionTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "collection",
			Name:      "total",
			Help:      "Total number of collection runs",
		},
		[]string{"collector", "status"},
	)

	// Snapshot metrics
	SnapshotsTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "snapshots",
			Name:      "total",
			Help:      "Total number of snapshots created",
		},
	)

	SnapshotsErrors = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "snapshots",
			Name:      "errors_total",
			Help:      "Total number of snapshot errors",
		},
	)

	// Analysis metrics
	AnalysisDuration = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "analysis",
			Name:      "duration_seconds",
			Help:      "Duration of analysis runs in seconds",
			Buckets:   []float64{0.1, 0.5, 1, 2, 5, 10, 30},
		},
	)

	AnalysisTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "analysis",
			Name:      "total",
			Help:      "Total number of analysis runs",
		},
		[]string{"status"},
	)

	IssuesDetected = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "analysis",
			Name:      "issues_detected",
			Help:      "Number of issues detected by severity",
		},
		[]string{"severity"},
	)

	// Suggestion metrics
	SuggestionsTotal = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "suggestions",
			Name:      "total",
			Help:      "Total number of active suggestions by severity",
		},
		[]string{"severity", "status"},
	)

	// Database metrics
	CacheHitRatio = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "database",
			Name:      "cache_hit_ratio",
			Help:      "Database cache hit ratio (0-1)",
		},
	)

	QueryCount = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "database",
			Name:      "query_count",
			Help:      "Number of unique queries tracked",
		},
	)

	SlowQueryCount = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "database",
			Name:      "slow_query_count",
			Help:      "Number of slow queries detected",
		},
	)

	// HTTP metrics
	HTTPRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "http",
			Name:      "requests_total",
			Help:      "Total number of HTTP requests",
		},
		[]string{"method", "path", "status"},
	)

	HTTPRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "http",
			Name:      "request_duration_seconds",
			Help:      "Duration of HTTP requests in seconds",
			Buckets:   []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1},
		},
		[]string{"method", "path"},
	)

	// Info metric
	BuildInfo = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "build_info",
			Help:      "Build information",
		},
		[]string{"version", "commit", "build_date"},
	)
)

// RecordBuildInfo sets the build info metric.
func RecordBuildInfo(version, commit, buildDate string) {
	BuildInfo.WithLabelValues(version, commit, buildDate).Set(1)
}

// RecordCollectionDuration records the duration of a collection run.
func RecordCollectionDuration(collector string, duration float64, success bool) {
	CollectionDuration.WithLabelValues(collector).Observe(duration)
	status := "success"
	if !success {
		status = "error"
	}
	CollectionTotal.WithLabelValues(collector, status).Inc()
}

// RecordSnapshot increments the snapshot counter.
func RecordSnapshot(success bool) {
	SnapshotsTotal.Inc()
	if !success {
		SnapshotsErrors.Inc()
	}
}

// RecordAnalysis records analysis metrics.
func RecordAnalysis(duration float64, success bool, issues map[string]int) {
	AnalysisDuration.Observe(duration)
	status := "success"
	if !success {
		status = "error"
	}
	AnalysisTotal.WithLabelValues(status).Inc()

	for severity, count := range issues {
		IssuesDetected.WithLabelValues(severity).Set(float64(count))
	}
}

// UpdateDatabaseMetrics updates database-related metrics.
func UpdateDatabaseMetrics(cacheRatio float64, queryCount, slowQueryCount int) {
	CacheHitRatio.Set(cacheRatio)
	QueryCount.Set(float64(queryCount))
	SlowQueryCount.Set(float64(slowQueryCount))
}

// UpdateSuggestionMetrics updates suggestion-related metrics.
func UpdateSuggestionMetrics(suggestions map[string]map[string]int) {
	// Reset all to 0 first
	SuggestionsTotal.Reset()
	for severity, statuses := range suggestions {
		for status, count := range statuses {
			SuggestionsTotal.WithLabelValues(severity, status).Set(float64(count))
		}
	}
}
