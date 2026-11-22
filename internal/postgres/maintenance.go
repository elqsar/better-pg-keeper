package postgres

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/elqsar/pganalyzer/internal/models"
)

// autovacuumValues holds the server-wide inputs to autovacuum's per-table
// trigger. Table reloptions override them in applyAutovacuumThresholds.
type autovacuumValues struct {
	vacuumBase   float64
	vacuumScale  float64
	analyzeBase  float64
	analyzeScale float64
	// vacuumMax caps the vacuum trigger when vacuumCapped is set
	// (autovacuum_vacuum_max_threshold, PostgreSQL 18+; -1 turns it off).
	// The zero value means no cap, as on servers that predate the setting.
	vacuumMax    float64
	vacuumCapped bool
	// disabled is set when autovacuum or track_counts is off server-wide, in
	// which case no table is vacuumed or analyzed by threshold.
	disabled bool
}

var requiredAutovacuumSettings = []string{
	"autovacuum_vacuum_threshold", "autovacuum_vacuum_scale_factor",
	"autovacuum_analyze_threshold", "autovacuum_analyze_scale_factor",
	"autovacuum", "track_counts",
}

func (c *PgxClient) autovacuumDefaults(ctx context.Context) (autovacuumValues, error) {
	var d autovacuumValues
	names := append(append([]string{}, requiredAutovacuumSettings...), "autovacuum_vacuum_max_threshold")
	rows, err := c.pool.Query(ctx, `SELECT name, setting FROM pg_settings WHERE name = ANY($1)`, names)
	if err != nil {
		return d, err
	}
	defer rows.Close()
	seen := 0
	for rows.Next() {
		var name, raw string
		if err := rows.Scan(&name, &raw); err != nil {
			return d, err
		}
		switch name {
		case "autovacuum", "track_counts":
			if raw != "on" {
				d.disabled = true
			}
			seen++
			continue
		}
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return d, fmt.Errorf("%s: %w", name, err)
		}
		switch name {
		case "autovacuum_vacuum_threshold":
			d.vacuumBase = v
		case "autovacuum_vacuum_scale_factor":
			d.vacuumScale = v
		case "autovacuum_analyze_threshold":
			d.analyzeBase = v
		case "autovacuum_analyze_scale_factor":
			d.analyzeScale = v
		case "autovacuum_vacuum_max_threshold":
			d.vacuumMax, d.vacuumCapped = v, v >= 0
			continue // optional: absent before PostgreSQL 18
		default:
			continue
		}
		seen++
	}
	if err := rows.Err(); err != nil {
		return d, err
	}
	if seen != len(requiredAutovacuumSettings) {
		return d, fmt.Errorf("expected %d autovacuum settings, got %d", len(requiredAutovacuumSettings), seen)
	}
	return d, nil
}

// relOptionFalse mirrors the spellings PostgreSQL accepts for a false boolean
// reloption, matching the autovacuum_disabled check in settings.go.
func relOptionFalse(value string) bool {
	switch strings.ToLower(value) {
	case "false", "off", "no", "0", "f", "n":
		return true
	}
	return false
}

func applyAutovacuumThresholds(s *models.TableStat, d autovacuumValues, options []string) {
	s.AutovacuumDisabled = d.disabled
	for _, option := range options {
		name, value, ok := strings.Cut(option, "=")
		if !ok {
			continue
		}
		if name == "autovacuum_enabled" {
			if relOptionFalse(value) {
				s.AutovacuumDisabled = true
			}
			continue
		}
		v, err := strconv.ParseFloat(value, 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			continue
		}
		switch name {
		case "autovacuum_vacuum_threshold":
			d.vacuumBase = v
		case "autovacuum_vacuum_scale_factor":
			d.vacuumScale = v
		case "autovacuum_analyze_threshold":
			d.analyzeBase = v
		case "autovacuum_analyze_scale_factor":
			d.analyzeScale = v
		case "autovacuum_vacuum_max_threshold":
			// A table value overrides the server's; -1 turns the cap off.
			d.vacuumMax, d.vacuumCapped = v, v >= 0
		}
	}
	if s.RelTuples < 0 {
		return
	} // pg_class has not yet sampled this relation.
	vacuum := d.vacuumBase + d.vacuumScale*s.RelTuples
	if d.vacuumCapped && vacuum > d.vacuumMax {
		vacuum = d.vacuumMax
	}
	analyze := d.analyzeBase + d.analyzeScale*s.RelTuples
	s.VacuumTrigger = &vacuum
	s.AnalyzeTrigger = &analyze
	s.VacuumScaleFactor = &d.vacuumScale
}
