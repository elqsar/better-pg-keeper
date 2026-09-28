package analyzer

import (
	"context"
	"fmt"
	"time"

	"github.com/elqsar/pganalyzer/internal/models"
)

// MainAnalyzer orchestrates all sub-analyzers to produce a complete analysis.
type MainAnalyzer struct {
	storage           Storage
	config            *Config
	slowQueryAnalyzer *SlowQueryAnalyzer
	cacheAnalyzer     *CacheAnalyzer
	tableAnalyzer     *TableAnalyzer
	indexAnalyzer     *IndexAnalyzer
}

// NewMainAnalyzer creates a new MainAnalyzer with all sub-analyzers.
func NewMainAnalyzer(storage Storage, cfg *Config) *MainAnalyzer {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	return &MainAnalyzer{
		storage:           storage,
		config:            cfg,
		slowQueryAnalyzer: NewSlowQueryAnalyzer(storage, cfg),
		cacheAnalyzer:     NewCacheAnalyzer(storage, cfg),
		tableAnalyzer:     NewTableAnalyzer(storage, cfg),
		indexAnalyzer:     NewIndexAnalyzer(storage, cfg),
	}
}

// resolveDomains determines, for each data domain, which snapshot last carried it
// and how old that data is.
//
// Collectors run on intervals from 30s to 1h, but the coordinator cuts a new snapshot
// roughly every minute, so the newest snapshot almost never holds table, index or
// bloat data. Reading every domain from a single snapshot therefore reported those
// domains as empty on nearly every run, which in turn made the suggester resolve
// still-active issues. Each domain is instead resolved to the most recent snapshot
// that actually contains it, bounded by the anchor snapshot so historical analysis
// stays historical.
func (a *MainAnalyzer) resolveDomains(ctx context.Context, anchor *models.Snapshot) (map[string]DomainCoverage, []string) {
	coverage := make(map[string]DomainCoverage, len(AllDomains))
	var errs []string
	now := time.Now()

	for _, domain := range AllDomains {
		snap, err := a.storage.GetLatestSnapshotWithCollector(ctx, anchor.InstanceID, domain, anchor.CapturedAt)
		if err != nil {
			// Leave the domain uncovered: that is the safe reading, but record the
			// error so a broken lookup is not mistaken for a quiet instance.
			errs = append(errs, fmt.Sprintf("resolving %s coverage: %v", domain, err))
			coverage[domain] = DomainCoverage{}
			continue
		}
		if snap == nil {
			coverage[domain] = DomainCoverage{}
			continue
		}

		runs, err := a.storage.GetSnapshotCollectors(ctx, snap.ID)
		if err != nil {
			errs = append(errs, fmt.Sprintf("resolving %s collection time: %v", domain, err))
			coverage[domain] = DomainCoverage{}
			continue
		}
		observedAt := time.Time{}
		for _, run := range runs {
			if run.Collector == domain && run.Status == models.CollectorStatusSuccess {
				observedAt = run.CollectedAt
				break
			}
		}
		if observedAt.IsZero() {
			coverage[domain] = DomainCoverage{}
			continue
		}
		age := now.Sub(observedAt)
		budget := a.config.StalenessBudget(domain)
		coverage[domain] = DomainCoverage{
			Present:    true,
			SnapshotID: snap.ID,
			CapturedAt: observedAt,
			Age:        age,
			Stale:      budget > 0 && age > budget,
		}
	}

	return coverage, errs
}

// snapshotFor returns the snapshot id holding a domain's data, or 0 when the domain
// has never been collected for this instance.
func snapshotFor(coverage map[string]DomainCoverage, domain string) int64 {
	return coverage[domain].SnapshotID
}

func invalidateDomain(coverage map[string]DomainCoverage, domain string) {
	entry := coverage[domain]
	entry.Stale = true
	coverage[domain] = entry
}

// Analyze runs all sub-analyzers and aggregates results.
// It handles partial failures gracefully - if one analyzer fails,
// others will still run and their results will be included.
//
// The passed snapshotID anchors the analysis in time and identifies the instance;
// each domain is then read from the most recent snapshot at or before it that
// actually holds that domain's data. See resolveDomains.
func (a *MainAnalyzer) Analyze(ctx context.Context, snapshotID int64) (*AnalysisResult, error) {
	// Get snapshot info
	snapshot, err := a.storage.GetSnapshotByID(ctx, snapshotID)
	if err != nil {
		return nil, fmt.Errorf("getting snapshot: %w", err)
	}
	if snapshot == nil {
		return nil, fmt.Errorf("snapshot %d not found", snapshotID)
	}

	coverage, coverageErrs := a.resolveDomains(ctx, snapshot)

	result := &AnalysisResult{
		SnapshotID: snapshotID,
		InstanceID: snapshot.InstanceID,
		AnalyzedAt: time.Now(),
		Coverage:   coverage,
		Errors:     coverageErrs,
		ErrorCount: len(coverageErrs),
	}

	// Run slow query and cache analysis
	if id := snapshotFor(coverage, DomainQueryStats); id != 0 {
		slowQueries, err := a.slowQueryAnalyzer.Analyze(ctx, id)
		if err != nil {
			invalidateDomain(coverage, DomainQueryStats)
			result.Errors = append(result.Errors, fmt.Sprintf("slow query analysis: %v", err))
			result.ErrorCount++
		} else {
			result.SlowQueries = slowQueries
		}

	}

	if id := snapshotFor(coverage, DomainDatabaseStats); id != 0 {
		cacheStats, err := a.cacheAnalyzer.AnalyzeOverall(ctx, id)
		if err != nil {
			invalidateDomain(coverage, DomainDatabaseStats)
			result.Errors = append(result.Errors, fmt.Sprintf("cache analysis: %v", err))
			result.ErrorCount++
		} else {
			if queryID := snapshotFor(coverage, DomainQueryStats); queryID != 0 && coverage[DomainQueryStats].Usable() {
				if err := a.cacheAnalyzer.AddQueryStats(ctx, queryID, cacheStats); err != nil {
					invalidateDomain(coverage, DomainQueryStats)
					result.Errors = append(result.Errors, fmt.Sprintf("query cache analysis: %v", err))
					result.ErrorCount++
				}
			}
			result.CacheStats = cacheStats
		}
	}

	// Run table analysis (bloat data usually lives in a different snapshot)
	if id := snapshotFor(coverage, DomainTableStats); id != 0 {
		tableIssues, err := a.tableAnalyzer.AnalyzeSnapshots(ctx, id, snapshotFor(coverage, DomainBloat))
		if err != nil {
			invalidateDomain(coverage, DomainBloat)
			result.Errors = append(result.Errors, fmt.Sprintf("table analysis: %v", err))
			result.ErrorCount++
			// A bloat read can fail while table statistics remain usable.
			tableIssues, err = a.tableAnalyzer.AnalyzeSnapshots(ctx, id, 0)
			if err != nil {
				invalidateDomain(coverage, DomainTableStats)
				result.Errors = append(result.Errors, fmt.Sprintf("table-only analysis: %v", err))
				result.ErrorCount++
			} else {
				result.TableIssues = tableIssues
			}
		} else {
			result.TableIssues = tableIssues
		}
	}

	// Run index analysis
	if id := snapshotFor(coverage, DomainIndexStats); id != 0 {
		indexIssues, err := a.indexAnalyzer.Analyze(ctx, id)
		if err != nil {
			invalidateDomain(coverage, DomainIndexStats)
			result.Errors = append(result.Errors, fmt.Sprintf("index analysis: %v", err))
			result.ErrorCount++
		} else {
			result.IndexIssues = indexIssues
		}
	}

	a.analyzeOperational(ctx, coverage, result)

	return result, nil
}

// analyzeOperational reads the latest covered operational samples and makes a
// failed or missing read ineligible to resolve existing suggestions.
func (a *MainAnalyzer) analyzeOperational(ctx context.Context, coverage map[string]DomainCoverage, result *AnalysisResult) {
	if id := snapshotFor(coverage, DomainActivity); id != 0 {
		activityStats, err := a.analyzeActivity(ctx, id)
		if err != nil {
			invalidateDomain(coverage, DomainActivity)
			result.Errors = append(result.Errors, fmt.Sprintf("activity analysis: %v", err))
			result.ErrorCount++
		} else {
			result.ActivityStats = activityStats
			if activityStats == nil {
				invalidateDomain(coverage, DomainActivity)
			} else if peak, err := a.analyzeConnectionPeak(ctx, result.InstanceID, coverage[DomainActivity].CapturedAt); err != nil {
				// The current sample is still valid; only the trend is missing.
				result.Errors = append(result.Errors, fmt.Sprintf("connection peak analysis: %v", err))
				result.ErrorCount++
			} else {
				activityStats.Peak = peak
			}
		}
	}

	if id := snapshotFor(coverage, DomainLocks); id != 0 {
		lockStats, err := a.analyzeLocks(ctx, id)
		if err != nil {
			invalidateDomain(coverage, DomainLocks)
			result.Errors = append(result.Errors, fmt.Sprintf("lock analysis: %v", err))
			result.ErrorCount++
		} else {
			result.LockStats = lockStats
			if lockStats == nil {
				invalidateDomain(coverage, DomainLocks)
			}
		}
	}

	if id := snapshotFor(coverage, DomainDatabaseStats); id != 0 {
		txStats, err := a.analyzeTransactions(ctx, id)
		if err != nil {
			invalidateDomain(coverage, DomainDatabaseStats)
			result.Errors = append(result.Errors, fmt.Sprintf("transaction analysis: %v", err))
			result.ErrorCount++
		} else {
			result.TransactionStats = txStats
			if txStats == nil {
				invalidateDomain(coverage, DomainDatabaseStats)
			}
		}
	}

	if id := snapshotFor(coverage, DomainQueryPlans); id != 0 {
		plans, err := a.analyzeQueryPlans(ctx, id)
		if err != nil {
			invalidateDomain(coverage, DomainQueryPlans)
			result.Errors = append(result.Errors, fmt.Sprintf("query plan analysis: %v", err))
			result.ErrorCount++
		} else {
			result.QueryPlans = plans
			if plans == nil {
				invalidateDomain(coverage, DomainQueryPlans)
			}
		}
	}

	if id := snapshotFor(coverage, DomainOutageRisk); id != 0 {
		risk, err := a.analyzeRisk(ctx, result.InstanceID, id, coverage)
		if err != nil {
			invalidateDomain(coverage, DomainOutageRisk)
			result.Errors = append(result.Errors, fmt.Sprintf("outage risk analysis: %v", err))
			result.ErrorCount++
		} else {
			result.Risk = risk
			if risk == nil {
				invalidateDomain(coverage, DomainOutageRisk)
			}
		}
	}

}

// AnalyzeWithTimeRange runs analysis using delta values between two snapshots.
// This is useful for analyzing recent performance over a specific time window.
func (a *MainAnalyzer) AnalyzeWithTimeRange(ctx context.Context, fromSnapshotID, toSnapshotID int64) (*AnalysisResult, error) {
	// Get target snapshot info
	snapshot, err := a.storage.GetSnapshotByID(ctx, toSnapshotID)
	if err != nil {
		return nil, fmt.Errorf("getting snapshot: %w", err)
	}
	if snapshot == nil {
		return nil, fmt.Errorf("snapshot %d not found", toSnapshotID)
	}

	coverage, coverageErrs := a.resolveDomains(ctx, snapshot)

	result := &AnalysisResult{
		SnapshotID: toSnapshotID,
		InstanceID: snapshot.InstanceID,
		AnalyzedAt: time.Now(),
		Coverage:   coverage,
		Errors:     coverageErrs,
		ErrorCount: len(coverageErrs),
	}

	// Run slow query analysis with deltas
	slowQueries, err := a.slowQueryAnalyzer.AnalyzeWithDeltas(ctx, fromSnapshotID, toSnapshotID)
	if err != nil {
		invalidateDomain(coverage, DomainQueryStats)
		result.Errors = append(result.Errors, fmt.Sprintf("slow query analysis: %v", err))
		result.ErrorCount++
	} else {
		result.SlowQueries = slowQueries
	}

	// The overall cache ratio is observed by the database collector, independently
	// of the query snapshots used for interval details.
	if id := snapshotFor(coverage, DomainDatabaseStats); id != 0 {
		cacheStats, err := a.cacheAnalyzer.AnalyzeOverall(ctx, id)
		if err != nil {
			invalidateDomain(coverage, DomainDatabaseStats)
			result.Errors = append(result.Errors, fmt.Sprintf("cache analysis: %v", err))
			result.ErrorCount++
		} else {
			if coverage[DomainQueryStats].Usable() {
				if err := a.cacheAnalyzer.AddQueryDeltas(ctx, fromSnapshotID, toSnapshotID, cacheStats); err != nil {
					invalidateDomain(coverage, DomainQueryStats)
					result.Errors = append(result.Errors, fmt.Sprintf("query cache analysis: %v", err))
					result.ErrorCount++
				}
			}
			result.CacheStats = cacheStats
		}
	}

	// Table and index analysis are point-in-time, not deltas, and each reads from
	// the most recent snapshot that actually carries its data.
	if id := snapshotFor(coverage, DomainTableStats); id != 0 {
		tableIssues, err := a.tableAnalyzer.AnalyzeSnapshots(ctx, id, snapshotFor(coverage, DomainBloat))
		if err != nil {
			invalidateDomain(coverage, DomainBloat)
			result.Errors = append(result.Errors, fmt.Sprintf("table analysis: %v", err))
			result.ErrorCount++
			tableIssues, err = a.tableAnalyzer.AnalyzeSnapshots(ctx, id, 0)
			if err != nil {
				invalidateDomain(coverage, DomainTableStats)
				result.Errors = append(result.Errors, fmt.Sprintf("table-only analysis: %v", err))
				result.ErrorCount++
			} else {
				result.TableIssues = tableIssues
			}
		} else {
			result.TableIssues = tableIssues
		}
	}

	if id := snapshotFor(coverage, DomainIndexStats); id != 0 {
		indexIssues, err := a.indexAnalyzer.Analyze(ctx, id)
		if err != nil {
			invalidateDomain(coverage, DomainIndexStats)
			result.Errors = append(result.Errors, fmt.Sprintf("index analysis: %v", err))
			result.ErrorCount++
		} else {
			result.IndexIssues = indexIssues
		}
	}
	a.analyzeOperational(ctx, coverage, result)

	return result, nil
}

// GetIssueCount returns the total count of all issues found.
func (r *AnalysisResult) GetIssueCount() int {
	count := len(r.SlowQueries) + len(r.TableIssues) + len(r.IndexIssues)
	if r.CacheStats != nil {
		count += len(r.CacheStats.PoorCacheQueries)
		if r.CacheStats.BelowThreshold {
			count++
		}
	}
	return count
}

// GetCriticalCount returns the count of critical severity issues.
func (r *AnalysisResult) GetCriticalCount() int {
	count := 0

	for _, issue := range r.TableIssues {
		if issue.Severity == "critical" {
			count++
		}
	}

	for _, issue := range r.IndexIssues {
		if issue.Severity == "critical" {
			count++
		}
	}

	return count
}

// GetWarningCount returns the count of warning severity issues.
func (r *AnalysisResult) GetWarningCount() int {
	count := 0

	// All slow queries are considered warnings
	count += len(r.SlowQueries)

	// Cache below threshold is a warning
	if r.CacheStats != nil && r.CacheStats.BelowThreshold {
		count++
	}

	for _, issue := range r.TableIssues {
		if issue.Severity == "warning" {
			count++
		}
	}

	for _, issue := range r.IndexIssues {
		if issue.Severity == "warning" {
			count++
		}
	}

	return count
}

// analyzeActivity analyzes connection activity from a snapshot.
func (a *MainAnalyzer) analyzeActivity(ctx context.Context, snapshotID int64) (*ActivityAnalysis, error) {
	activity, err := a.storage.GetConnectionActivity(ctx, snapshotID)
	if err != nil {
		return nil, fmt.Errorf("getting connection activity: %w", err)
	}
	if activity == nil {
		return nil, nil // No activity data collected
	}

	analysis := &ActivityAnalysis{
		TotalConnections: activity.TotalConnections,
		MaxConnections:   activity.MaxConnections,
		ActiveCount:      activity.ActiveCount,
		IdleCount:        activity.IdleCount,
		IdleInTxCount:    activity.IdleInTxCount,
		WaitingCount:     activity.WaitingCount,
	}

	// Calculate connection utilization
	if activity.MaxConnections > 0 {
		analysis.ConnectionUtilization = float64(activity.TotalConnections) / float64(activity.MaxConnections) * 100
	}

	// Get long-running queries
	longRunning, err := a.storage.GetLongRunningQueries(ctx, snapshotID)
	if err != nil {
		return nil, fmt.Errorf("getting long running queries: %w", err)
	}
	analysis.LongRunningQueries = longRunning

	// Get idle-in-transaction connections
	idleInTx, err := a.storage.GetIdleInTransaction(ctx, snapshotID)
	if err != nil {
		return nil, fmt.Errorf("getting idle in transaction: %w", err)
	}
	analysis.IdleInTransaction = idleInTx

	return analysis, nil
}

// analyzeLocks analyzes lock statistics from a snapshot.
func (a *MainAnalyzer) analyzeLocks(ctx context.Context, snapshotID int64) (*LockAnalysis, error) {
	stats, err := a.storage.GetLockStats(ctx, snapshotID)
	if err != nil {
		return nil, fmt.Errorf("getting lock stats: %w", err)
	}
	if stats == nil {
		return nil, nil // No lock data collected
	}

	analysis := &LockAnalysis{
		TotalLocks:   stats.TotalLocks,
		GrantedLocks: stats.GrantedLocks,
		WaitingLocks: stats.WaitingLocks,
	}

	// Get blocked queries
	blocked, err := a.storage.GetBlockedQueries(ctx, snapshotID)
	if err != nil {
		return nil, fmt.Errorf("getting blocked queries: %w", err)
	}
	analysis.BlockedQueries = blocked

	// Determine if lock contention is high (>5 waiting locks or any blocked queries)
	analysis.LockContentionHigh = stats.WaitingLocks > 5 || len(blocked) > 0

	return analysis, nil
}

// analyzeTransactions analyzes transaction statistics from a snapshot.
func (a *MainAnalyzer) analyzeTransactions(ctx context.Context, snapshotID int64) (*TransactionAnalysis, error) {
	stats, err := a.storage.GetExtendedDatabaseStats(ctx, snapshotID)
	if err != nil {
		return nil, fmt.Errorf("getting extended database stats: %w", err)
	}
	if stats == nil {
		return nil, nil // No transaction data collected
	}

	return &TransactionAnalysis{
		XactCommit:   stats.XactCommit,
		XactRollback: stats.XactRollback,
		TempFiles:    stats.TempFiles,
		TempBytes:    stats.TempBytes,
		Deadlocks:    stats.Deadlocks,
	}, nil
}

// Ensure MainAnalyzer implements Analyzer interface.
var _ Analyzer = (*MainAnalyzer)(nil)
