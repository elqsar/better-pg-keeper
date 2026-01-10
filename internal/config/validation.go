package config

import (
	"errors"
	"fmt"
	"strings"
)

// ValidationError represents a configuration validation error.
type ValidationError struct {
	Field   string
	Message string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// ValidationErrors is a collection of validation errors.
type ValidationErrors []ValidationError

func (e ValidationErrors) Error() string {
	if len(e) == 0 {
		return "no validation errors"
	}
	if len(e) == 1 {
		return e[0].Error()
	}

	var msgs []string
	for _, err := range e {
		msgs = append(msgs, err.Error())
	}
	return fmt.Sprintf("multiple validation errors:\n  - %s", strings.Join(msgs, "\n  - "))
}

// Validate checks the configuration for errors and returns descriptive messages.
func Validate(cfg *Config) error {
	var errs ValidationErrors

	// Validate PostgreSQL config
	errs = append(errs, validatePostgres(&cfg.Postgres)...)

	// Validate Storage config
	errs = append(errs, validateStorage(&cfg.Storage)...)

	// Validate Server config
	errs = append(errs, validateServer(&cfg.Server)...)

	// Validate Scheduler config
	errs = append(errs, validateScheduler(&cfg.Scheduler)...)

	// Validate Thresholds config
	errs = append(errs, validateThresholds(&cfg.Thresholds)...)

	if len(errs) > 0 {
		return errs
	}
	return nil
}

func validatePostgres(cfg *PostgresConfig) ValidationErrors {
	var errs ValidationErrors

	if cfg.Host == "" {
		errs = append(errs, ValidationError{
			Field:   "postgres.host",
			Message: "host is required",
		})
	}

	if cfg.Database == "" {
		errs = append(errs, ValidationError{
			Field:   "postgres.database",
			Message: "database is required",
		})
	}

	if cfg.User == "" {
		errs = append(errs, ValidationError{
			Field:   "postgres.user",
			Message: "user is required",
		})
	}

	if cfg.Port < 1 || cfg.Port > 65535 {
		errs = append(errs, ValidationError{
			Field:   "postgres.port",
			Message: fmt.Sprintf("port must be between 1 and 65535, got %d", cfg.Port),
		})
	}

	validSSLModes := map[string]bool{
		"disable": true,
		"allow":   true,
		"prefer":  true,
		"require": true,
	}
	if cfg.SSLMode != "" && !validSSLModes[cfg.SSLMode] {
		errs = append(errs, ValidationError{
			Field:   "postgres.sslmode",
			Message: fmt.Sprintf("invalid sslmode %q, must be one of: disable, allow, prefer, require", cfg.SSLMode),
		})
	}

	return errs
}

func validateStorage(cfg *StorageConfig) ValidationErrors {
	var errs ValidationErrors

	if cfg.Path == "" {
		errs = append(errs, ValidationError{
			Field:   "storage.path",
			Message: "path is required",
		})
	}

	if cfg.Retention.Snapshots <= 0 {
		errs = append(errs, ValidationError{
			Field:   "storage.retention.snapshots",
			Message: "snapshots retention must be positive",
		})
	}

	if cfg.Retention.QueryStats <= 0 {
		errs = append(errs, ValidationError{
			Field:   "storage.retention.query_stats",
			Message: "query_stats retention must be positive",
		})
	}

	return errs
}

func validateServer(cfg *ServerConfig) ValidationErrors {
	var errs ValidationErrors

	if cfg.Port < 1 || cfg.Port > 65535 {
		errs = append(errs, ValidationError{
			Field:   "server.port",
			Message: fmt.Sprintf("port must be between 1 and 65535, got %d", cfg.Port),
		})
	}

	if cfg.Auth.Enabled {
		if cfg.Auth.Username == "" {
			errs = append(errs, ValidationError{
				Field:   "server.auth.username",
				Message: "username is required when auth is enabled",
			})
		}
		if cfg.Auth.Password == "" {
			errs = append(errs, ValidationError{
				Field:   "server.auth.password",
				Message: "password is required when auth is enabled",
			})
		}
	}

	return errs
}

func validateScheduler(cfg *SchedulerConfig) ValidationErrors {
	var errs ValidationErrors

	if cfg.SnapshotInterval <= 0 {
		errs = append(errs, ValidationError{
			Field:   "scheduler.snapshot_interval",
			Message: "snapshot_interval must be positive",
		})
	}

	if cfg.AnalysisInterval <= 0 {
		errs = append(errs, ValidationError{
			Field:   "scheduler.analysis_interval",
			Message: "analysis_interval must be positive",
		})
	}

	return errs
}

func validateThresholds(cfg *ThresholdsConfig) ValidationErrors {
	var errs ValidationErrors

	if cfg.SlowQueryMs <= 0 {
		errs = append(errs, ValidationError{
			Field:   "thresholds.slow_query_ms",
			Message: "slow_query_ms must be positive",
		})
	}

	if cfg.CacheHitRatioWarning < 0 || cfg.CacheHitRatioWarning > 1 {
		errs = append(errs, ValidationError{
			Field:   "thresholds.cache_hit_ratio_warning",
			Message: fmt.Sprintf("cache_hit_ratio_warning must be between 0 and 1, got %f", cfg.CacheHitRatioWarning),
		})
	}

	if cfg.BloatPercentWarning < 0 || cfg.BloatPercentWarning > 100 {
		errs = append(errs, ValidationError{
			Field:   "thresholds.bloat_percent_warning",
			Message: fmt.Sprintf("bloat_percent_warning must be between 0 and 100, got %d", cfg.BloatPercentWarning),
		})
	}

	if cfg.UnusedIndexDays <= 0 {
		errs = append(errs, ValidationError{
			Field:   "thresholds.unused_index_days",
			Message: "unused_index_days must be positive",
		})
	}

	if cfg.SeqScanRatioWarning < 0 || cfg.SeqScanRatioWarning > 1 {
		errs = append(errs, ValidationError{
			Field:   "thresholds.seq_scan_ratio_warning",
			Message: fmt.Sprintf("seq_scan_ratio_warning must be between 0 and 1, got %f", cfg.SeqScanRatioWarning),
		})
	}

	if cfg.MinTableSizeForIndex <= 0 {
		errs = append(errs, ValidationError{
			Field:   "thresholds.min_table_size_for_index",
			Message: "min_table_size_for_index must be positive",
		})
	}

	return errs
}

// IsRequired checks if an error indicates a required field is missing.
func IsRequired(err error) bool {
	var validationErrs ValidationErrors
	if errors.As(err, &validationErrs) {
		for _, ve := range validationErrs {
			if strings.Contains(ve.Message, "required") {
				return true
			}
		}
	}
	var validationErr ValidationError
	if errors.As(err, &validationErr) {
		return strings.Contains(validationErr.Message, "required")
	}
	return false
}
