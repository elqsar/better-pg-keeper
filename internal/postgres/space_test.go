package postgres

import (
	"testing"

	"github.com/elqsar/pganalyzer/internal/models"
)

func TestRoughDeadTupleBytes(t *testing.T) {
	b := models.BloatInfo{HeapBytes: 1000, NLiveTup: 80, NDeadTup: 20}
	got, ok := roughDeadTupleBytes(b)
	if !ok || got != 200 {
		t.Fatalf("got %d, %t; want 200, true", got, ok)
	}
	if _, ok := roughDeadTupleBytes(models.BloatInfo{HeapBytes: 1000}); ok {
		t.Fatal("missing row counts must remain unknown")
	}
}

func TestSelectHeapCandidatesIsBoundedAndIncludesLargeCleanHeaps(t *testing.T) {
	stats := make([]models.BloatInfo, 20)
	for i := range stats {
		dead := int64(i + 1)
		stats[i] = models.BloatInfo{HeapBytes: 100 << 20, DeadTupleBytes: &dead}
	}
	stats[0].HeapBytes = 2 << 30 // clean but large
	indexes := selectHeapCandidates(stats)
	if len(indexes) != 10 {
		t.Fatalf("selected %d, want 10", len(indexes))
	}
	seen := map[int]bool{}
	for _, i := range indexes {
		seen[i] = true
	}
	if !seen[0] || !seen[19] {
		t.Fatalf("expected largest heap and dead footprint, got %v", indexes)
	}
}
