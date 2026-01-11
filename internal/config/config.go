// Package config provides configuration loading and validation for pganalyzer.
package config

import "time"

// Config is the root configuration structure for pganalyzer.
type Config struct {
	Postgres   PostgresConfig   `yaml:"postgres"`
	Storage    StorageConfig    `yaml:"storage"`
	Scheduler  SchedulerConfig  `yaml:"scheduler"`
	Server     ServerConfig     `yaml:"server"`
	Thresholds ThresholdsConfig `yaml:"thresholds"`
	Logging    LoggingConfig    `yaml:"logging"`
	Metrics    MetricsConfig    `yaml:"metrics"`
}

// LoggingConfig contains logging settings.
type LoggingConfig struct {
	Level    string `yaml:"level"`    // debug, info, warn, error
	Format   string `yaml:"format"`   // text, json
	Requests bool   `yaml:"requests"` // Log HTTP requests
}

// MetricsConfig contains Prometheus metrics settings.
type MetricsConfig struct {
	Enabled bool   `yaml:"enabled"` // Enable metrics endpoint
	Path    string `yaml:"path"`    // Metrics endpoint path (default: /metrics)
}

// PostgresConfig contains PostgreSQL connection settings.
type PostgresConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Database string `yaml:"database"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	SSLMode  string `yaml:"sslmode"`
}

// StorageConfig contains SQLite storage settings.
type StorageConfig struct {
	Path      string          `yaml:"path"`
	Retention RetentionConfig `yaml:"retention"`
}

// RetentionConfig contains data retention settings.
type RetentionConfig struct {
	Snapshots  Duration `yaml:"snapshots"`
	QueryStats Duration `yaml:"query_stats"`
}

// SchedulerConfig contains collection scheduler settings.
type SchedulerConfig struct {
	SnapshotInterval Duration `yaml:"snapshot_interval"`
	AnalysisInterval Duration `yaml:"analysis_interval"`
}

// ServerConfig contains HTTP server settings.
type ServerConfig struct {
	Host string     `yaml:"host"`
	Port int        `yaml:"port"`
	Auth AuthConfig `yaml:"auth"`
}

// AuthConfig contains authentication settings.
type AuthConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

// ThresholdsConfig contains analysis threshold settings.
type ThresholdsConfig struct {
	SlowQueryMs          int     `yaml:"slow_query_ms"`
	CacheHitRatioWarning float64 `yaml:"cache_hit_ratio_warning"`
	BloatPercentWarning  int     `yaml:"bloat_percent_warning"`
	UnusedIndexDays      int     `yaml:"unused_index_days"`
	SeqScanRatioWarning  float64 `yaml:"seq_scan_ratio_warning"`
	MinTableSizeForIndex int     `yaml:"min_table_size_for_index"`
}

// Duration is a wrapper around time.Duration that supports YAML unmarshaling.
type Duration time.Duration

// UnmarshalYAML implements yaml.Unmarshaler for Duration.
func (d *Duration) UnmarshalYAML(unmarshal func(any) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	*d = Duration(parsed)
	return nil
}

// MarshalYAML implements yaml.Marshaler for Duration.
func (d Duration) MarshalYAML() (any, error) {
	return time.Duration(d).String(), nil
}

// Duration returns the underlying time.Duration.
func (d Duration) Duration() time.Duration {
	return time.Duration(d)
}

// String returns the string representation of the duration.
func (d Duration) String() string {
	return time.Duration(d).String()
}

// Default returns a Config with sensible default values.
func Default() Config {
	return Config{
		Postgres: PostgresConfig{
			Port:    5432,
			SSLMode: "prefer",
		},
		Storage: StorageConfig{
			Path: "./data/pganalyzer.db",
			Retention: RetentionConfig{
				Snapshots:  Duration(168 * time.Hour), // 7 days
				QueryStats: Duration(720 * time.Hour), // 30 days
			},
		},
		Scheduler: SchedulerConfig{
			SnapshotInterval: Duration(5 * time.Minute),
			AnalysisInterval: Duration(15 * time.Minute),
		},
		Server: ServerConfig{
			Host: "0.0.0.0",
			Port: 8080,
			Auth: AuthConfig{
				Enabled: true,
			},
		},
		Thresholds: ThresholdsConfig{
			SlowQueryMs:          1000,
			CacheHitRatioWarning: 0.95,
			BloatPercentWarning:  20,
			UnusedIndexDays:      30,
			SeqScanRatioWarning:  0.5,
			MinTableSizeForIndex: 10000,
		},
		Logging: LoggingConfig{
			Level:    "info",
			Format:   "text",
			Requests: true,
		},
		Metrics: MetricsConfig{
			Enabled: false,
			Path:    "/metrics",
		},
	}
}
