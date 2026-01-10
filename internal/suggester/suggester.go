package suggester

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/user/pganalyzer/internal/analyzer"
	"github.com/user/pganalyzer/internal/models"
)

// Storage defines the storage interface needed by the suggester.
type Storage interface {
	UpsertSuggestion(ctx context.Context, sug *models.Suggestion) error
	GetActiveSuggestions(ctx context.Context, instanceID int64) ([]models.Suggestion, error)
	ResolveSuggestion(ctx context.Context, id int64) error
}

// Suggester runs rules against analysis results and manages suggestions.
type Suggester struct {
	rules   []Rule
	storage Storage
	config  *Config
	logger  *log.Logger
}

// NewSuggester creates a new Suggester with the given configuration.
func NewSuggester(storage Storage, config *Config, logger *log.Logger) *Suggester {
	if config == nil {
		config = DefaultConfig()
	}
	if logger == nil {
		logger = log.Default()
	}

	return &Suggester{
		rules:   make([]Rule, 0),
		storage: storage,
		config:  config,
		logger:  logger,
	}
}

// RegisterRule adds a rule to the suggester.
func (s *Suggester) RegisterRule(rule Rule) {
	s.rules = append(s.rules, rule)
}

// RegisterRules adds multiple rules to the suggester.
func (s *Suggester) RegisterRules(rules ...Rule) {
	s.rules = append(s.rules, rules...)
}

// Rules returns the registered rules.
func (s *Suggester) Rules() []Rule {
	return s.rules
}

// Config returns the suggester configuration.
func (s *Suggester) Config() *Config {
	return s.config
}

// SuggestResult contains the results of running the suggester.
type SuggestResult struct {
	TotalSuggestions int      // Number of suggestions generated
	NewSuggestions   int      // Number of new suggestions
	UpdatedCount     int      // Number of existing suggestions updated
	ResolvedCount    int      // Number of issues that are now resolved
	Errors           []string // Errors encountered during suggestion generation
}

// Suggest runs all rules against the analysis results and updates suggestions.
func (s *Suggester) Suggest(ctx context.Context, analysis *analyzer.AnalysisResult) (*SuggestResult, error) {
	if analysis == nil {
		return nil, fmt.Errorf("analysis result is nil")
	}

	result := &SuggestResult{}
	instanceID := analysis.InstanceID

	// Collect all suggestions from rules
	var allSuggestions []Suggestion

	for _, rule := range s.rules {
		suggestions, err := rule.Evaluate(ctx, analysis)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("rule %s: %v", rule.ID(), err))
			s.logger.Printf("Error evaluating rule %s: %v", rule.ID(), err)
			continue
		}
		allSuggestions = append(allSuggestions, suggestions...)
	}

	result.TotalSuggestions = len(allSuggestions)

	// Get existing active suggestions to track resolved ones
	existingSuggestions, err := s.storage.GetActiveSuggestions(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("getting active suggestions: %w", err)
	}

	// Build a map of existing suggestions by (rule_id, target_object)
	existingMap := make(map[string]*models.Suggestion)
	for i := range existingSuggestions {
		sug := &existingSuggestions[i]
		key := suggestionKey(sug.RuleID, sug.TargetObject)
		existingMap[key] = sug
	}

	// Track which existing suggestions are still active
	stillActive := make(map[string]bool)

	// Upsert all new suggestions
	for _, sug := range allSuggestions {
		key := suggestionKey(sug.RuleID, sug.TargetObject)
		stillActive[key] = true

		modelSug, err := sug.ToModel(instanceID)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("converting suggestion: %v", err))
			continue
		}

		// Check if this is a new or updated suggestion
		if _, exists := existingMap[key]; !exists {
			result.NewSuggestions++
		} else {
			result.UpdatedCount++
		}

		if err := s.storage.UpsertSuggestion(ctx, modelSug); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("upserting suggestion: %v", err))
			s.logger.Printf("Error upserting suggestion: %v", err)
		}
	}

	// Mark resolved suggestions (issues that are no longer detected)
	for key, sug := range existingMap {
		if !stillActive[key] {
			// This issue is no longer detected, mark as resolved
			if err := s.storage.ResolveSuggestion(ctx, sug.ID); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("resolving suggestion %d: %v", sug.ID, err))
				s.logger.Printf("Error resolving suggestion %d: %v", sug.ID, err)
			} else {
				result.ResolvedCount++
			}
		}
	}

	return result, nil
}

// suggestionKey creates a unique key for deduplication.
func suggestionKey(ruleID, targetObject string) string {
	return ruleID + ":" + targetObject
}

// GetActiveSuggestions retrieves all active suggestions for an instance.
func (s *Suggester) GetActiveSuggestions(ctx context.Context, instanceID int64) ([]models.Suggestion, error) {
	return s.storage.GetActiveSuggestions(ctx, instanceID)
}

// SuggestionStats contains statistics about suggestions.
type SuggestionStats struct {
	Total    int
	Critical int
	Warning  int
	Info     int
}

// GetSuggestionStats returns statistics for active suggestions.
func (s *Suggester) GetSuggestionStats(ctx context.Context, instanceID int64) (*SuggestionStats, error) {
	suggestions, err := s.storage.GetActiveSuggestions(ctx, instanceID)
	if err != nil {
		return nil, err
	}

	stats := &SuggestionStats{
		Total: len(suggestions),
	}

	for _, sug := range suggestions {
		switch sug.Severity {
		case models.SeverityCritical:
			stats.Critical++
		case models.SeverityWarning:
			stats.Warning++
		case models.SeverityInfo:
			stats.Info++
		}
	}

	return stats, nil
}

// CleanupOldResolved marks old resolved suggestions for cleanup.
// This is useful for housekeeping - suggestions that have been resolved
// for longer than the retention period can be purged.
func (s *Suggester) CleanupOldResolved(ctx context.Context, instanceID int64, retentionDays int) (int, error) {
	// Note: This would require additional storage methods to implement.
	// For now, this is a placeholder for future implementation.
	_ = retentionDays
	_ = time.Now() // Would use for comparison with last_seen_at
	return 0, nil
}
