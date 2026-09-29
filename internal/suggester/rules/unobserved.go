package rules

import (
	"sort"

	"github.com/elqsar/pganalyzer/internal/analyzer"
)

// unobservedTargets maps failed checks to the target prefixes they hide. A rule
// lists, per check it reads, the prefix of the targets built from that check.
func unobservedTargets(unavailable map[string]string, prefixByCheck map[string]string) []string {
	var out []string
	for check, prefix := range prefixByCheck {
		if _, failed := unavailable[check]; failed {
			out = append(out, prefix)
		}
	}
	sort.Strings(out)
	return out
}

// riskUnavailable returns the failed outage-risk checks, nil without data.
func riskUnavailable(analysis *analyzer.AnalysisResult) map[string]string {
	if analysis == nil || analysis.Risk == nil || analysis.Risk.OutageRisk == nil {
		return nil
	}
	return analysis.Risk.Unavailable
}

// settingsUnavailable returns the failed settings checks, nil without data.
func settingsUnavailable(analysis *analyzer.AnalysisResult) map[string]string {
	if analysis == nil || analysis.Settings == nil || analysis.Settings.ServerSettings == nil {
		return nil
	}
	return analysis.Settings.Unavailable
}
