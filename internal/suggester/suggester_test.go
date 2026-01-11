package suggester_test

import (
	"context"
	"log"
	"os"
	"testing"
	"time"

	"github.com/elqsar/pganalyzer/internal/analyzer"
	"github.com/elqsar/pganalyzer/internal/models"
	"github.com/elqsar/pganalyzer/internal/suggester"
	"github.com/elqsar/pganalyzer/internal/suggester/rules"
)

// mockStorage implements the Storage interface for testing.
type mockStorage struct {
	suggestions map[int64][]models.Suggestion
	nextID      int64
}

func newMockStorage() *mockStorage {
	return &mockStorage{
		suggestions: make(map[int64][]models.Suggestion),
		nextID:      1,
	}
}

func (m *mockStorage) UpsertSuggestion(ctx context.Context, sug *models.Suggestion) error {
	instanceID := sug.InstanceID

	// Check if suggestion already exists
	for i, existing := range m.suggestions[instanceID] {
		if existing.RuleID == sug.RuleID && existing.TargetObject == sug.TargetObject {
			// Update existing
			m.suggestions[instanceID][i].Severity = sug.Severity
			m.suggestions[instanceID][i].Title = sug.Title
			m.suggestions[instanceID][i].Description = sug.Description
			m.suggestions[instanceID][i].Metadata = sug.Metadata
			m.suggestions[instanceID][i].LastSeenAt = time.Now()
			if m.suggestions[instanceID][i].Status == models.StatusResolved {
				m.suggestions[instanceID][i].Status = models.StatusActive
			}
			return nil
		}
	}

	// Insert new
	sug.ID = m.nextID
	m.nextID++
	sug.Status = models.StatusActive
	sug.FirstSeenAt = time.Now()
	sug.LastSeenAt = time.Now()
	m.suggestions[instanceID] = append(m.suggestions[instanceID], *sug)
	return nil
}

func (m *mockStorage) GetSuggestionsByStatus(ctx context.Context, instanceID int64, status string) ([]models.Suggestion, error) {
	var filtered []models.Suggestion
	for _, sug := range m.suggestions[instanceID] {
		if sug.Status == status {
			filtered = append(filtered, sug)
		}
	}
	return filtered, nil
}

func (m *mockStorage) ResolveSuggestion(ctx context.Context, id int64) error {
	for instanceID := range m.suggestions {
		for i, sug := range m.suggestions[instanceID] {
			if sug.ID == id {
				m.suggestions[instanceID][i].Status = models.StatusResolved
				return nil
			}
		}
	}
	return nil
}

func TestSlowQueryRule_Evaluate(t *testing.T) {
	config := suggester.DefaultConfig()
	rule := rules.NewSlowQueryRule(config)

	tests := []struct {
		name           string
		analysis       *analyzer.AnalysisResult
		wantCount      int
		wantSeverities []string
	}{
		{
			name:      "nil analysis",
			analysis:  nil,
			wantCount: 0,
		},
		{
			name: "no slow queries",
			analysis: &analyzer.AnalysisResult{
				SlowQueries: nil,
			},
			wantCount: 0,
		},
		{
			name: "warning level slow query",
			analysis: &analyzer.AnalysisResult{
				SlowQueries: []analyzer.SlowQuery{
					{
						QueryID:       12345,
						Query:         "SELECT * FROM users WHERE id = $1",
						MeanExecTime:  1500, // 1.5s
						MaxExecTime:   3000,
						TotalExecTime: 15000,
						Calls:         10,
						CacheHitRatio: 0.98,
						AvgRows:       1,
					},
				},
			},
			wantCount:      1,
			wantSeverities: []string{suggester.SeverityWarning},
		},
		{
			name: "critical level slow query",
			analysis: &analyzer.AnalysisResult{
				SlowQueries: []analyzer.SlowQuery{
					{
						QueryID:       12345,
						Query:         "SELECT * FROM large_table WHERE status = $1",
						MeanExecTime:  6000, // 6s
						MaxExecTime:   10000,
						TotalExecTime: 60000,
						Calls:         10,
						CacheHitRatio: 0.85,
						AvgRows:       5000,
					},
				},
			},
			wantCount:      1,
			wantSeverities: []string{suggester.SeverityCritical},
		},
		{
			name: "multiple slow queries",
			analysis: &analyzer.AnalysisResult{
				SlowQueries: []analyzer.SlowQuery{
					{
						QueryID:      1,
						Query:        "SELECT * FROM users",
						MeanExecTime: 1500,
						Calls:        10,
					},
					{
						QueryID:      2,
						Query:        "SELECT * FROM orders",
						MeanExecTime: 7000,
						Calls:        5,
					},
				},
			},
			wantCount:      2,
			wantSeverities: []string{suggester.SeverityWarning, suggester.SeverityCritical},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suggestions, err := rule.Evaluate(context.Background(), tt.analysis)
			if err != nil {
				t.Fatalf("Evaluate() error = %v", err)
			}
			if len(suggestions) != tt.wantCount {
				t.Errorf("Evaluate() got %d suggestions, want %d", len(suggestions), tt.wantCount)
			}
			for i, sev := range tt.wantSeverities {
				if i < len(suggestions) && suggestions[i].Severity != sev {
					t.Errorf("Suggestion[%d] severity = %s, want %s", i, suggestions[i].Severity, sev)
				}
			}
		})
	}
}

func TestUnusedIndexRule_Evaluate(t *testing.T) {
	config := suggester.DefaultConfig()
	rule := rules.NewUnusedIndexRule(config)

	tests := []struct {
		name      string
		analysis  *analyzer.AnalysisResult
		wantCount int
	}{
		{
			name:      "nil analysis",
			analysis:  nil,
			wantCount: 0,
		},
		{
			name: "no index issues",
			analysis: &analyzer.AnalysisResult{
				IndexIssues: nil,
			},
			wantCount: 0,
		},
		{
			name: "unused index detected",
			analysis: &analyzer.AnalysisResult{
				IndexIssues: []analyzer.IndexIssue{
					{
						SchemaName:   "public",
						TableName:    "users",
						IndexName:    "idx_users_legacy",
						IssueType:    analyzer.IndexIssueUnused,
						Severity:     "warning",
						IndexSize:    1024 * 1024,
						IdxScan:      0,
						IsUnique:     false,
						IsPrimary:    false,
						SpaceSavings: 1024 * 1024,
					},
				},
			},
			wantCount: 1,
		},
		{
			name: "skip primary key",
			analysis: &analyzer.AnalysisResult{
				IndexIssues: []analyzer.IndexIssue{
					{
						SchemaName: "public",
						TableName:  "users",
						IndexName:  "users_pkey",
						IssueType:  analyzer.IndexIssueUnused,
						IdxScan:    0,
						IsPrimary:  true,
					},
				},
			},
			wantCount: 0,
		},
		{
			name: "skip unique index",
			analysis: &analyzer.AnalysisResult{
				IndexIssues: []analyzer.IndexIssue{
					{
						SchemaName: "public",
						TableName:  "users",
						IndexName:  "idx_users_email_unique",
						IssueType:  analyzer.IndexIssueUnused,
						IdxScan:    0,
						IsUnique:   true,
					},
				},
			},
			wantCount: 0,
		},
		{
			name: "skip duplicate issue type",
			analysis: &analyzer.AnalysisResult{
				IndexIssues: []analyzer.IndexIssue{
					{
						SchemaName: "public",
						TableName:  "users",
						IndexName:  "idx_users_dup",
						IssueType:  analyzer.IndexIssueDuplicate,
					},
				},
			},
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suggestions, err := rule.Evaluate(context.Background(), tt.analysis)
			if err != nil {
				t.Fatalf("Evaluate() error = %v", err)
			}
			if len(suggestions) != tt.wantCount {
				t.Errorf("Evaluate() got %d suggestions, want %d", len(suggestions), tt.wantCount)
			}
		})
	}
}

func TestMissingIndexRule_Evaluate(t *testing.T) {
	config := suggester.DefaultConfig()
	rule := rules.NewMissingIndexRule(config)

	tests := []struct {
		name      string
		analysis  *analyzer.AnalysisResult
		wantCount int
	}{
		{
			name:      "nil analysis",
			analysis:  nil,
			wantCount: 0,
		},
		{
			name: "missing index on large table",
			analysis: &analyzer.AnalysisResult{
				TableIssues: []analyzer.TableIssue{
					{
						SchemaName:   "public",
						TableName:    "orders",
						IssueType:    analyzer.TableIssueMissingIndex,
						TableSize:    1024 * 1024, // 1MB
						SeqScanRatio: 0.85,
						NLiveTup:     100000,
					},
				},
			},
			wantCount: 1,
		},
		{
			name: "skip small table",
			analysis: &analyzer.AnalysisResult{
				TableIssues: []analyzer.TableIssue{
					{
						SchemaName:   "public",
						TableName:    "config",
						IssueType:    analyzer.TableIssueMissingIndex,
						TableSize:    100, // 100 bytes
						SeqScanRatio: 0.9,
						NLiveTup:     10,
					},
				},
			},
			wantCount: 0,
		},
		{
			name: "skip other issue types",
			analysis: &analyzer.AnalysisResult{
				TableIssues: []analyzer.TableIssue{
					{
						SchemaName: "public",
						TableName:  "logs",
						IssueType:  analyzer.TableIssueHighBloat,
						TableSize:  1024 * 1024,
					},
				},
			},
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suggestions, err := rule.Evaluate(context.Background(), tt.analysis)
			if err != nil {
				t.Fatalf("Evaluate() error = %v", err)
			}
			if len(suggestions) != tt.wantCount {
				t.Errorf("Evaluate() got %d suggestions, want %d", len(suggestions), tt.wantCount)
			}
		})
	}
}

func TestBloatRule_Evaluate(t *testing.T) {
	config := suggester.DefaultConfig()
	rule := rules.NewBloatRule(config)

	tests := []struct {
		name         string
		analysis     *analyzer.AnalysisResult
		wantCount    int
		wantSeverity string
	}{
		{
			name:      "nil analysis",
			analysis:  nil,
			wantCount: 0,
		},
		{
			name: "high bloat warning",
			analysis: &analyzer.AnalysisResult{
				TableIssues: []analyzer.TableIssue{
					{
						SchemaName:   "public",
						TableName:    "events",
						IssueType:    analyzer.TableIssueHighBloat,
						CurrentValue: 25, // 25% bloat
						NDeadTup:     25000,
						NLiveTup:     100000,
					},
				},
			},
			wantCount:    1,
			wantSeverity: suggester.SeverityWarning,
		},
		{
			name: "critical bloat",
			analysis: &analyzer.AnalysisResult{
				TableIssues: []analyzer.TableIssue{
					{
						SchemaName:   "public",
						TableName:    "logs",
						IssueType:    analyzer.TableIssueHighBloat,
						CurrentValue: 60, // 60% bloat
						NDeadTup:     60000,
						NLiveTup:     100000,
					},
				},
			},
			wantCount:    1,
			wantSeverity: suggester.SeverityCritical,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suggestions, err := rule.Evaluate(context.Background(), tt.analysis)
			if err != nil {
				t.Fatalf("Evaluate() error = %v", err)
			}
			if len(suggestions) != tt.wantCount {
				t.Errorf("Evaluate() got %d suggestions, want %d", len(suggestions), tt.wantCount)
			}
			if tt.wantCount > 0 && len(suggestions) > 0 && suggestions[0].Severity != tt.wantSeverity {
				t.Errorf("Suggestion severity = %s, want %s", suggestions[0].Severity, tt.wantSeverity)
			}
		})
	}
}

func TestVacuumRule_Evaluate(t *testing.T) {
	config := suggester.DefaultConfig()
	rule := rules.NewVacuumRule(config)

	oldVacuum := time.Now().Add(-14 * 24 * time.Hour)

	tests := []struct {
		name      string
		analysis  *analyzer.AnalysisResult
		wantCount int
	}{
		{
			name:      "nil analysis",
			analysis:  nil,
			wantCount: 0,
		},
		{
			name: "stale vacuum detected",
			analysis: &analyzer.AnalysisResult{
				TableIssues: []analyzer.TableIssue{
					{
						SchemaName: "public",
						TableName:  "sessions",
						IssueType:  analyzer.TableIssueStaleVacuum,
						LastVacuum: &oldVacuum,
						NDeadTup:   50000,
						NLiveTup:   100000,
					},
				},
			},
			wantCount: 1,
		},
		{
			name: "never vacuumed",
			analysis: &analyzer.AnalysisResult{
				TableIssues: []analyzer.TableIssue{
					{
						SchemaName: "public",
						TableName:  "new_table",
						IssueType:  analyzer.TableIssueStaleVacuum,
						LastVacuum: nil,
						NDeadTup:   10000,
						NLiveTup:   50000,
					},
				},
			},
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suggestions, err := rule.Evaluate(context.Background(), tt.analysis)
			if err != nil {
				t.Fatalf("Evaluate() error = %v", err)
			}
			if len(suggestions) != tt.wantCount {
				t.Errorf("Evaluate() got %d suggestions, want %d", len(suggestions), tt.wantCount)
			}
		})
	}
}

func TestCacheRule_Evaluate(t *testing.T) {
	config := suggester.DefaultConfig()
	rule := rules.NewCacheRule(config)

	tests := []struct {
		name         string
		analysis     *analyzer.AnalysisResult
		wantCount    int
		wantSeverity string
	}{
		{
			name:      "nil analysis",
			analysis:  nil,
			wantCount: 0,
		},
		{
			name: "good cache ratio",
			analysis: &analyzer.AnalysisResult{
				CacheStats: &analyzer.CacheAnalysis{
					OverallHitRatio: 99.5,
					BelowThreshold:  false,
				},
			},
			wantCount: 0,
		},
		{
			name: "warning level cache ratio",
			analysis: &analyzer.AnalysisResult{
				CacheStats: &analyzer.CacheAnalysis{
					OverallHitRatio: 93, // 93%
					BelowThreshold:  true,
					Threshold:       95,
				},
			},
			wantCount:    1,
			wantSeverity: suggester.SeverityWarning,
		},
		{
			name: "critical cache ratio",
			analysis: &analyzer.AnalysisResult{
				CacheStats: &analyzer.CacheAnalysis{
					OverallHitRatio: 85, // 85%
					BelowThreshold:  true,
					Threshold:       95,
					PoorCacheQueries: []analyzer.PoorCacheQuery{
						{QueryID: 1, Query: "SELECT * FROM big_table", CacheHitRatio: 0.6},
					},
				},
			},
			wantCount:    1,
			wantSeverity: suggester.SeverityCritical,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suggestions, err := rule.Evaluate(context.Background(), tt.analysis)
			if err != nil {
				t.Fatalf("Evaluate() error = %v", err)
			}
			if len(suggestions) != tt.wantCount {
				t.Errorf("Evaluate() got %d suggestions, want %d", len(suggestions), tt.wantCount)
			}
			if tt.wantCount > 0 && len(suggestions) > 0 && suggestions[0].Severity != tt.wantSeverity {
				t.Errorf("Suggestion severity = %s, want %s", suggestions[0].Severity, tt.wantSeverity)
			}
		})
	}
}

func TestSuggester_Suggest(t *testing.T) {
	logger := log.New(os.Stdout, "", 0)
	storage := newMockStorage()
	config := suggester.DefaultConfig()

	s := suggester.NewSuggester(storage, config, logger)
	s.RegisterRules(
		rules.NewSlowQueryRule(config),
		rules.NewUnusedIndexRule(config),
		rules.NewBloatRule(config),
		rules.NewCacheRule(config),
	)

	tests := []struct {
		name       string
		analysis   *analyzer.AnalysisResult
		wantTotal  int
		wantNew    int
		wantErrors int
	}{
		{
			name: "mixed issues",
			analysis: &analyzer.AnalysisResult{
				InstanceID: 1,
				SlowQueries: []analyzer.SlowQuery{
					{
						QueryID:      1,
						Query:        "SELECT * FROM slow_table",
						MeanExecTime: 2000,
					},
				},
				IndexIssues: []analyzer.IndexIssue{
					{
						SchemaName: "public",
						TableName:  "users",
						IndexName:  "idx_unused",
						IssueType:  analyzer.IndexIssueUnused,
						IsUnique:   false,
						IsPrimary:  false,
					},
				},
				TableIssues: []analyzer.TableIssue{
					{
						SchemaName:   "public",
						TableName:    "logs",
						IssueType:    analyzer.TableIssueHighBloat,
						CurrentValue: 30,
					},
				},
			},
			wantTotal: 3,
			wantNew:   3,
		},
		{
			name: "no issues",
			analysis: &analyzer.AnalysisResult{
				InstanceID: 2,
			},
			wantTotal: 0,
			wantNew:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := s.Suggest(context.Background(), tt.analysis)
			if err != nil {
				t.Fatalf("Suggest() error = %v", err)
			}
			if result.TotalSuggestions != tt.wantTotal {
				t.Errorf("TotalSuggestions = %d, want %d", result.TotalSuggestions, tt.wantTotal)
			}
			if result.NewSuggestions != tt.wantNew {
				t.Errorf("NewSuggestions = %d, want %d", result.NewSuggestions, tt.wantNew)
			}
			if len(result.Errors) != tt.wantErrors {
				t.Errorf("Errors = %d, want %d", len(result.Errors), tt.wantErrors)
			}
		})
	}
}

func TestSuggester_Deduplication(t *testing.T) {
	logger := log.New(os.Stdout, "", 0)
	storage := newMockStorage()
	config := suggester.DefaultConfig()

	s := suggester.NewSuggester(storage, config, logger)
	s.RegisterRule(rules.NewSlowQueryRule(config))

	analysis := &analyzer.AnalysisResult{
		InstanceID: 1,
		SlowQueries: []analyzer.SlowQuery{
			{
				QueryID:      123,
				Query:        "SELECT * FROM users",
				MeanExecTime: 2000,
			},
		},
	}

	// First run - should create new suggestion
	result1, err := s.Suggest(context.Background(), analysis)
	if err != nil {
		t.Fatalf("First Suggest() error = %v", err)
	}
	if result1.NewSuggestions != 1 {
		t.Errorf("First run NewSuggestions = %d, want 1", result1.NewSuggestions)
	}

	// Second run - should update existing suggestion
	result2, err := s.Suggest(context.Background(), analysis)
	if err != nil {
		t.Fatalf("Second Suggest() error = %v", err)
	}
	if result2.NewSuggestions != 0 {
		t.Errorf("Second run NewSuggestions = %d, want 0", result2.NewSuggestions)
	}
	if result2.UpdatedCount != 1 {
		t.Errorf("Second run UpdatedCount = %d, want 1", result2.UpdatedCount)
	}

	// Verify only one suggestion exists
	suggestions, _ := storage.GetSuggestionsByStatus(context.Background(), 1, models.StatusActive)
	if len(suggestions) != 1 {
		t.Errorf("Active suggestions = %d, want 1", len(suggestions))
	}
}

func TestSuggester_ResolveGone(t *testing.T) {
	logger := log.New(os.Stdout, "", 0)
	storage := newMockStorage()
	config := suggester.DefaultConfig()

	s := suggester.NewSuggester(storage, config, logger)
	s.RegisterRule(rules.NewSlowQueryRule(config))

	// First run with slow query
	analysis1 := &analyzer.AnalysisResult{
		InstanceID: 1,
		SlowQueries: []analyzer.SlowQuery{
			{QueryID: 123, Query: "SELECT * FROM users", MeanExecTime: 2000},
		},
	}
	_, err := s.Suggest(context.Background(), analysis1)
	if err != nil {
		t.Fatalf("First Suggest() error = %v", err)
	}

	// Second run without the slow query (issue resolved)
	analysis2 := &analyzer.AnalysisResult{
		InstanceID:  1,
		SlowQueries: nil,
	}
	result2, err := s.Suggest(context.Background(), analysis2)
	if err != nil {
		t.Fatalf("Second Suggest() error = %v", err)
	}
	if result2.ResolvedCount != 1 {
		t.Errorf("ResolvedCount = %d, want 1", result2.ResolvedCount)
	}

	// Verify no active suggestions
	suggestions, _ := storage.GetSuggestionsByStatus(context.Background(), 1, models.StatusActive)
	if len(suggestions) != 0 {
		t.Errorf("Active suggestions = %d, want 0", len(suggestions))
	}
}

func TestSuggester_GetSuggestionStats(t *testing.T) {
	logger := log.New(os.Stdout, "", 0)
	storage := newMockStorage()
	config := suggester.DefaultConfig()

	s := suggester.NewSuggester(storage, config, logger)
	s.RegisterRules(
		rules.NewSlowQueryRule(config),
		rules.NewCacheRule(config),
	)

	analysis := &analyzer.AnalysisResult{
		InstanceID: 1,
		SlowQueries: []analyzer.SlowQuery{
			{QueryID: 1, Query: "SELECT 1", MeanExecTime: 2000},  // warning
			{QueryID: 2, Query: "SELECT 2", MeanExecTime: 10000}, // critical
		},
		CacheStats: &analyzer.CacheAnalysis{
			OverallHitRatio: 85,
			BelowThreshold:  true,
		},
	}

	_, err := s.Suggest(context.Background(), analysis)
	if err != nil {
		t.Fatalf("Suggest() error = %v", err)
	}

	stats, err := s.GetSuggestionStats(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetSuggestionStats() error = %v", err)
	}

	if stats.Total != 3 {
		t.Errorf("Total = %d, want 3", stats.Total)
	}
	if stats.Critical != 2 { // critical slow query + critical cache
		t.Errorf("Critical = %d, want 2", stats.Critical)
	}
	if stats.Warning != 1 { // warning slow query
		t.Errorf("Warning = %d, want 1", stats.Warning)
	}
}

func TestSuggestion_ToModel(t *testing.T) {
	sug := &suggester.Suggestion{
		RuleID:       "test_rule",
		Severity:     suggester.SeverityWarning,
		Title:        "Test Title",
		Description:  "Test Description",
		TargetObject: "test.target",
		Metadata: map[string]interface{}{
			"key1": "value1",
			"key2": 123,
		},
	}

	model, err := sug.ToModel(1)
	if err != nil {
		t.Fatalf("ToModel() error = %v", err)
	}

	if model.InstanceID != 1 {
		t.Errorf("InstanceID = %d, want 1", model.InstanceID)
	}
	if model.RuleID != "test_rule" {
		t.Errorf("RuleID = %s, want test_rule", model.RuleID)
	}
	if model.Severity != suggester.SeverityWarning {
		t.Errorf("Severity = %s, want %s", model.Severity, suggester.SeverityWarning)
	}
	if model.Title != "Test Title" {
		t.Errorf("Title = %s, want Test Title", model.Title)
	}
	if model.Metadata == "" {
		t.Error("Metadata is empty, expected JSON")
	}
}

func TestDefaultConfig(t *testing.T) {
	config := suggester.DefaultConfig()

	if config.SlowQueryMs != 1000 {
		t.Errorf("SlowQueryMs = %f, want 1000", config.SlowQueryMs)
	}
	if config.SlowQueryCriticalMs != 5000 {
		t.Errorf("SlowQueryCriticalMs = %f, want 5000", config.SlowQueryCriticalMs)
	}
	if config.CacheHitRatioWarning != 0.95 {
		t.Errorf("CacheHitRatioWarning = %f, want 0.95", config.CacheHitRatioWarning)
	}
	if config.BloatPercentWarning != 20 {
		t.Errorf("BloatPercentWarning = %f, want 20", config.BloatPercentWarning)
	}
	if config.UnusedIndexDays != 30 {
		t.Errorf("UnusedIndexDays = %d, want 30", config.UnusedIndexDays)
	}
}

func TestSuggester_NilAnalysis(t *testing.T) {
	logger := log.New(os.Stdout, "", 0)
	storage := newMockStorage()
	config := suggester.DefaultConfig()

	s := suggester.NewSuggester(storage, config, logger)

	_, err := s.Suggest(context.Background(), nil)
	if err == nil {
		t.Error("Expected error for nil analysis")
	}
}
