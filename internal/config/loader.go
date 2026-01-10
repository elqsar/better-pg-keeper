package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// DefaultConfigPath is the default path to look for configuration file.
const DefaultConfigPath = "./config.yaml"

// EnvConfigPath is the environment variable that can override the config path.
const EnvConfigPath = "PGANALYZER_CONFIG"

// Load reads and parses the configuration from the given path.
// If path is empty, it tries PGANALYZER_CONFIG env var, then DefaultConfigPath.
// It expands environment variables in the format ${VAR_NAME} within string values.
func Load(path string) (*Config, error) {
	// Determine config file path
	configPath := path
	if configPath == "" {
		configPath = os.Getenv(EnvConfigPath)
	}
	if configPath == "" {
		configPath = DefaultConfigPath
	}

	// Read the config file
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("reading config file %s: %w", configPath, err)
	}

	// Expand environment variables in the YAML content
	expanded := expandEnvVars(string(data))

	// Start with defaults
	cfg := Default()

	// Parse YAML into the config struct
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	// Validate the configuration
	if err := Validate(&cfg); err != nil {
		return nil, fmt.Errorf("validating config: %w", err)
	}

	return &cfg, nil
}

// envVarPattern matches ${VAR_NAME} or ${VAR_NAME:-default} patterns.
var envVarPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?::-([^}]*))?\}`)

// expandEnvVars replaces ${VAR_NAME} patterns with their environment variable values.
// It supports default values with ${VAR_NAME:-default} syntax.
func expandEnvVars(content string) string {
	return envVarPattern.ReplaceAllStringFunc(content, func(match string) string {
		// Extract variable name and optional default
		submatches := envVarPattern.FindStringSubmatch(match)
		if len(submatches) < 2 {
			return match
		}

		varName := submatches[1]
		defaultVal := ""
		if len(submatches) >= 3 {
			defaultVal = submatches[2]
		}

		// Get the environment variable value
		value := os.Getenv(varName)
		if value == "" && defaultVal != "" {
			value = defaultVal
		}

		return value
	})
}

// MustLoad is like Load but panics on error.
func MustLoad(path string) *Config {
	cfg, err := Load(path)
	if err != nil {
		panic(err)
	}
	return cfg
}

// LoadFromString parses configuration from a YAML string.
// Useful for testing.
func LoadFromString(content string) (*Config, error) {
	// Expand environment variables
	expanded := expandEnvVars(content)

	// Start with defaults
	cfg := Default()

	// Parse YAML
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	// Validate
	if err := Validate(&cfg); err != nil {
		return nil, fmt.Errorf("validating config: %w", err)
	}

	return &cfg, nil
}

// LoadFromStringNoValidation parses configuration without validation.
// Useful for testing validation separately.
func LoadFromStringNoValidation(content string) (*Config, error) {
	expanded := expandEnvVars(content)
	cfg := Default()
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	return &cfg, nil
}

// ExpandEnvVars is exported for testing purposes.
func ExpandEnvVars(content string) string {
	return expandEnvVars(content)
}

// ParseDuration parses a duration string like "5m", "1h30m", etc.
// This is a helper that can be used to parse duration strings outside of YAML.
func ParseDuration(s string) (Duration, error) {
	var d Duration
	if err := yaml.Unmarshal([]byte(s), &d); err != nil {
		return 0, err
	}
	return d, nil
}

// FormatConnectionString creates a PostgreSQL connection string from the config.
func (c *PostgresConfig) FormatConnectionString() string {
	var parts []string

	if c.Host != "" {
		parts = append(parts, fmt.Sprintf("host=%s", c.Host))
	}
	if c.Port > 0 {
		parts = append(parts, fmt.Sprintf("port=%d", c.Port))
	}
	if c.Database != "" {
		parts = append(parts, fmt.Sprintf("dbname=%s", c.Database))
	}
	if c.User != "" {
		parts = append(parts, fmt.Sprintf("user=%s", c.User))
	}
	if c.Password != "" {
		parts = append(parts, fmt.Sprintf("password=%s", c.Password))
	}
	if c.SSLMode != "" {
		parts = append(parts, fmt.Sprintf("sslmode=%s", c.SSLMode))
	}

	return strings.Join(parts, " ")
}
