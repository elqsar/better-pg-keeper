package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/elqsar/pganalyzer/internal/models"
)

// CheckSetup reports whether the server and the monitoring role give
// PGAnalyzer what it needs, with a fix for each problem. Checks are
// independent; one that cannot run is reported as failed with its error.
func (c *PgxClient) CheckSetup(ctx context.Context) (*models.SetupReport, error) {
	if c.pool == nil {
		return nil, fmt.Errorf("postgres: not connected")
	}
	report := &models.SetupReport{CheckedAt: time.Now()}
	for _, check := range []func(context.Context) models.SetupCheck{
		c.setupVersion,
		c.setupStatStatements,
		c.setupPrivileges,
		c.setupHypoPG,
	} {
		report.Checks = append(report.Checks, check(ctx))
	}

	// Unused-index evidence counts from the last statistics reset, or from
	// server start when a crash discarded statistics along with the reset time.
	var since time.Time
	if err := c.pool.QueryRow(ctx, `
		SELECT COALESCE(stats_reset, pg_postmaster_start_time())
		FROM pg_stat_database WHERE datname = current_database()
	`).Scan(&since); err == nil {
		report.StatsSince = &since
	}
	return report, nil
}

func (c *PgxClient) setupVersion(ctx context.Context) models.SetupCheck {
	check := models.SetupCheck{Name: "version", Title: "PostgreSQL version"}
	num, err := c.GetServerVersionNum(ctx)
	if err != nil {
		check.Status, check.Detail = models.SetupFail, err.Error()
		return check
	}
	version := fmt.Sprintf("%d.%d", num/10000, num%10000)
	if num < MinServerVersionNum {
		check.Status = models.SetupFail
		check.Detail = fmt.Sprintf("PostgreSQL %s is not supported; PGAnalyzer needs %d or later.", version, MinServerVersionNum/10000)
		check.Fix = "Upgrade the server (pg_upgrade, or the managed service's major version upgrade)."
		return check
	}
	check.Status, check.Detail = models.SetupOK, "PostgreSQL "+version
	return check
}

func (c *PgxClient) setupStatStatements(ctx context.Context) models.SetupCheck {
	check := models.SetupCheck{Name: "pg_stat_statements", Title: "pg_stat_statements"}
	var installed bool
	var db, schema, user string
	if err := c.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pg_stat_statements'), current_database(),
		       COALESCE((SELECT n.nspname FROM pg_extension e JOIN pg_namespace n ON n.oid = e.extnamespace
		                 WHERE e.extname = 'pg_stat_statements'), ''),
		       current_user
	`).Scan(&installed, &db, &schema, &user); err != nil {
		check.Status, check.Detail = models.SetupFail, err.Error()
		return check
	}
	preloadFix := "Add it to shared_preload_libraries and restart:\n" +
		"ALTER SYSTEM SET shared_preload_libraries = 'pg_stat_statements';  -- keep any libraries already listed\n" +
		"On managed Postgres, set it in the parameter group or database flags; it is often preloaded already."
	if !installed {
		check.Status = models.SetupFail
		check.Detail = fmt.Sprintf("The extension is not created in database %q, so no query statistics can be collected.", db)
		check.Fix = "CREATE EXTENSION pg_stat_statements;\nIf that fails with \"must be loaded via shared_preload_libraries\": " + preloadFix
		return check
	}
	// Reading the view fails unless the library was preloaded at server start.
	// This works for any role, unlike reading shared_preload_libraries.
	var entries int64
	if err := c.pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_statements`).Scan(&entries); err != nil {
		check.Status = models.SetupFail
		switch {
		case strings.Contains(err.Error(), "shared_preload_libraries"):
			check.Detail = "The extension exists but the library is not loaded, so it records nothing."
			check.Fix = preloadFix
		case strings.Contains(err.Error(), "SQLSTATE 42P01"):
			// Installed in a schema the role doesn't search, as on Supabase.
			check.Detail = fmt.Sprintf("The extension is in schema %q, which is not on %q's search_path.", schema, user)
			check.Fix = fmt.Sprintf("ALTER ROLE %s SET search_path = \"$user\", public, %s;\nPGAnalyzer picks it up on its next connection; restart it to apply now.",
				quoteIdentifier(user), quoteIdentifier(schema))
		default:
			check.Detail = "Reading pg_stat_statements failed: " + err.Error()
		}
		return check
	}
	check.Status = models.SetupOK
	check.Detail = fmt.Sprintf("Installed in %q, %d statements tracked.", db, entries)
	return check
}

func (c *PgxClient) setupPrivileges(ctx context.Context) models.SetupCheck {
	check := models.SetupCheck{Name: "privileges", Title: "Monitoring privileges"}
	var user string
	var super, monitor, stats, settings bool
	if err := c.pool.QueryRow(ctx, `
		SELECT current_user,
		       COALESCE((SELECT rolsuper FROM pg_roles WHERE rolname = current_user), false),
		       pg_has_role(current_user, 'pg_monitor', 'USAGE'),
		       pg_has_role(current_user, 'pg_read_all_stats', 'USAGE'),
		       pg_has_role(current_user, 'pg_read_all_settings', 'USAGE')
	`).Scan(&user, &super, &monitor, &stats, &settings); err != nil {
		check.Status, check.Detail = models.SetupFail, err.Error()
		return check
	}
	switch {
	case super:
		check.Status = models.SetupOK
		check.Detail = fmt.Sprintf("%q is a superuser. A dedicated role with pg_monitor is safer.", user)
	case monitor || (stats && settings):
		check.Status = models.SetupOK
		check.Detail = fmt.Sprintf("%q can read all statistics and settings.", user)
	default:
		var missing []string
		if !stats {
			missing = append(missing, "other roles' query texts and session details are hidden")
		}
		if !settings {
			missing = append(missing, "some settings can't be read")
		}
		check.Status = models.SetupWarn
		check.Detail = fmt.Sprintf("%q lacks pg_monitor: %s. Queries from other roles show as <insufficient privilege>.", user, strings.Join(missing, "; "))
		check.Fix = fmt.Sprintf("GRANT pg_monitor TO %s;", quoteIdentifier(user))
	}
	return check
}

func (c *PgxClient) setupHypoPG(ctx context.Context) models.SetupCheck {
	check := models.SetupCheck{Name: "hypopg", Title: "hypopg (optional)"}
	var installed, available bool
	if err := c.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'hypopg'),
		       EXISTS (SELECT 1 FROM pg_available_extensions WHERE name = 'hypopg')
	`).Scan(&installed, &available); err != nil {
		check.Status, check.Detail = models.SetupInfo, err.Error()
		return check
	}
	switch {
	case installed:
		check.Status, check.Detail = models.SetupOK, "Index proposals are checked against hypothetical indexes."
	case available:
		check.Status = models.SetupInfo
		check.Detail = "Available but not created. With it, index proposals show the planner's cost before and after."
		check.Fix = "CREATE EXTENSION hypopg;"
	default:
		check.Status = models.SetupInfo
		check.Detail = "Not available on this server. Index proposals are shown without a cost check."
	}
	return check
}

func quoteIdentifier(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}
