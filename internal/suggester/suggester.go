package suggester

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/elqsar/pganalyzer/internal/analyzer"
	"github.com/elqsar/pganalyzer/internal/models"
)

// Storage defines the storage interface needed by the suggester.
type Storage interface {
	UpsertSuggestion(ctx context.Context, sug *models.Suggestion) error
	GetSuggestionsByStatus(ctx context.Context, instanceID int64, status string) ([]models.Suggestion, error)
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
	SkippedRules     []string // Rules skipped because their data was missing or stale
	Errors           []string // Errors encountered during suggestion generation
}

// Suggest runs all rules against the analysis results and updates suggestions.
func (s *Suggester) Suggest(ctx context.Context, analysis *analyzer.AnalysisResult) (*SuggestResult, error) {
	if analysis == nil {
		return nil, fmt.Errorf("analysis result is nil")
	}

	result := &SuggestResult{}
	instanceID := analysis.InstanceID

	// Collect all suggestions from rules whose data was actually observed.
	//
	// A rule whose backing domain is missing or stale must be skipped entirely, not
	// evaluated against empty data: the analysis result cannot distinguish "the issue
	// is gone" from "nothing was collected", and treating the second as the first
	// resolves live issues on every collection gap.
	var allSuggestions []Suggestion
	resolvableRules := make(map[string]bool, len(s.rules))
	unobserved := make(map[string][]string)

	for _, rule := range s.rules {
		if !analysis.DomainsUsable(rule.RequiredDomains()...) {
			result.SkippedRules = append(result.SkippedRules, rule.ID())
			s.logger.Printf("Skipping rule %s: required data %v not usable (coverage: %s)",
				rule.ID(), rule.RequiredDomains(), describeCoverage(analysis, rule.RequiredDomains()))
			continue
		}
		resolvableRules[rule.ID()] = true

		suggestions, err := rule.Evaluate(ctx, analysis)
		if err != nil {
			// The rule ran but failed, so its absence of suggestions proves nothing.
			delete(resolvableRules, rule.ID())
			result.Errors = append(result.Errors, fmt.Sprintf("rule %s: %v", rule.ID(), err))
			s.logger.Printf("Error evaluating rule %s: %v", rule.ID(), err)
			continue
		}
		allSuggestions = append(allSuggestions, suggestions...)
		if partial, ok := rule.(PartiallyObserved); ok {
			if prefixes := partial.UnobservedTargets(analysis); len(prefixes) > 0 {
				unobserved[rule.ID()] = prefixes
				s.logger.Printf("Rule %s: not resolving targets %v, their data was not observed", rule.ID(), prefixes)
			}
		}
	}

	result.TotalSuggestions = len(allSuggestions)

	// Get existing active suggestions to track resolved ones
	existingSuggestions, err := s.storage.GetSuggestionsByStatus(ctx, instanceID, models.StatusActive)
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

	// Mark resolved suggestions (issues that are no longer detected).
	// Only suggestions from rules that actually ran this cycle are eligible: for a
	// skipped or failed rule, "not detected" means "not looked for".
	for key, sug := range existingMap {
		if !stillActive[key] {
			if !resolvableRules[sug.RuleID] || hasAnyPrefix(sug.TargetObject, unobserved[sug.RuleID]) {
				continue
			}
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

// describeCoverage renders the coverage of the given domains for logging.
func describeCoverage(analysis *analyzer.AnalysisResult, domains []string) string {
	parts := make([]string, 0, len(domains))
	for _, d := range domains {
		c := analysis.DomainCoverage(d)
		switch {
		case !c.Present:
			parts = append(parts, d+"=missing")
		case c.Stale:
			parts = append(parts, fmt.Sprintf("%s=stale(%s old)", d, c.Age.Truncate(time.Second)))
		default:
			parts = append(parts, fmt.Sprintf("%s=ok(%s old)", d, c.Age.Truncate(time.Second)))
		}
	}
	return strings.Join(parts, ", ")
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// suggestionKey creates a unique key for deduplication.
func suggestionKey(ruleID, targetObject string) string {
	return ruleID + ":" + targetObject
}

// GetActiveSuggestions retrieves all active suggestions for an instance.
func (s *Suggester) GetActiveSuggestions(ctx context.Context, instanceID int64) ([]models.Suggestion, error) {
	return s.storage.GetSuggestionsByStatus(ctx, instanceID, models.StatusActive)
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
	suggestions, err := s.storage.GetSuggestionsByStatus(ctx, instanceID, models.StatusActive)
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
