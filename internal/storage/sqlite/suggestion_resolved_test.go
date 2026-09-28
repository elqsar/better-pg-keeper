package sqlite

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/elqsar/pganalyzer/internal/models"
)

func TestSuggestionResolvedAt(t *testing.T) {
	storage := setupTestStorage(t)
	ctx := context.Background()
	instID, err := storage.CreateInstance(ctx, &models.Instance{Name: "r", Host: "h", Port: 5432, Database: "d"})
	if err != nil {
		t.Fatal(err)
	}
	sug := &models.Suggestion{InstanceID: instID, RuleID: "slow_query", Severity: models.SeverityWarning,
		Title: "t", Description: "d", TargetObject: "query:1", Metadata: "{}"}
	if err := storage.UpsertSuggestion(ctx, sug); err != nil {
		t.Fatal(err)
	}
	active, err := storage.GetSuggestionsByStatus(ctx, instID, models.StatusActive)
	if err != nil || len(active) != 1 || active[0].ResolvedAt != nil {
		t.Fatalf("active = %+v, %v", active, err)
	}
	id := active[0].ID

	before := time.Now()
	if err := storage.ResolveSuggestion(ctx, id); err != nil {
		t.Fatal(err)
	}
	got, err := storage.GetSuggestionByID(ctx, id)
	if err != nil || got.Status != models.StatusResolved || got.ResolvedAt == nil || got.ResolvedAt.Before(before.Add(-time.Second)) {
		t.Fatalf("after resolve = %+v, %v", got, err)
	}
	first := *got.ResolvedAt

	// Resolving again keeps the original time.
	time.Sleep(10 * time.Millisecond)
	if err := storage.ResolveSuggestion(ctx, id); err != nil {
		t.Fatal(err)
	}
	if got, _ := storage.GetSuggestionByID(ctx, id); got.ResolvedAt == nil || !got.ResolvedAt.Equal(first) {
		t.Errorf("second resolve moved resolved_at: %v -> %v", first, got.ResolvedAt)
	}

	// The issue coming back reactivates it and clears the time.
	if err := storage.UpsertSuggestion(ctx, sug); err != nil {
		t.Fatal(err)
	}
	if got, _ := storage.GetSuggestionByID(ctx, id); got.Status != models.StatusActive || got.ResolvedAt != nil {
		t.Errorf("after reactivation = %+v", got)
	}
}

// TestResolvedAtMigrationBackfill checks that 021 strips monotonic suffixes
// and backfills resolved_at for suggestions resolved before it existed.
func TestResolvedAtMigrationBackfill(t *testing.T) {
	storage := setupTestStorage(t)
	ctx := context.Background()
	instID, err := storage.CreateInstance(ctx, &models.Instance{Name: "m", Host: "h", Port: 5432, Database: "d"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := storage.writeDB.ExecContext(ctx, `
		INSERT INTO suggestions (instance_id, rule_id, severity, title, description, target_object, metadata,
			status, first_seen_at, last_seen_at)
		VALUES (?, 'slow_query', 'warning', 't', 'd', 'q', '{}', 'resolved',
			'2026-09-01 10:00:00 +0000 UTC m=+1.5', '2026-09-02 10:00:00 +0000 UTC m=+86401.5')
	`, instID); err != nil {
		t.Fatal(err)
	}

	up, err := migrationsFS.ReadFile("migrations/021_suggestion_resolved_at.sql")
	if err != nil {
		t.Fatal(err)
	}
	// Re-run only the data statements of the Up section.
	section := strings.SplitN(strings.SplitN(string(up), "-- +migrate Down", 2)[0], "ADD COLUMN resolved_at DATETIME;", 2)[1]
	if _, err := storage.writeDB.ExecContext(ctx, section); err != nil {
		t.Fatal(err)
	}

	got, err := storage.GetSuggestionsByStatus(ctx, instID, models.StatusResolved)
	if err != nil || len(got) != 1 {
		t.Fatalf("resolved = %+v, %v", got, err)
	}
	want := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	if got[0].ResolvedAt == nil || !got[0].ResolvedAt.Equal(want) || !got[0].LastSeenAt.Equal(want) {
		t.Errorf("resolved_at = %v, last_seen_at = %v; want %v", got[0].ResolvedAt, got[0].LastSeenAt, want)
	}
	var raw string
	if err := storage.readDB.QueryRowContext(ctx, `SELECT resolved_at FROM suggestions`).Scan(&raw); err != nil || strings.Contains(raw, " m=") {
		t.Errorf("resolved_at stored as %q, %v", raw, err)
	}
}
