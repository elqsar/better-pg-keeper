package config

import (
	"fmt"
	"os"
	"reflect"
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

	// Parse YAML into AST, expand environment variables on scalar values only,
	// and decode into defaults to prevent YAML injection from env values.
	cfg, err := unmarshalConfig(data)
	if err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	// Validate the configuration
	if err := Validate(&cfg); err != nil {
		return nil, fmt.Errorf("validating config: %w", err)
	}

	return &cfg, nil
}

var durationType = reflect.TypeOf(Duration(0))

func unmarshalConfig(data []byte) (Config, error) {
	cfg := Default()
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return cfg, err
	}
	if doc.Kind == 0 || len(doc.Content) == 0 {
		return cfg, nil
	}
	expandNodeEnvVars(&doc, reflect.TypeOf(cfg))
	if err := doc.Decode(&cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// expandNodeEnvVars walks a parsed YAML AST guided by the target Go type and
// expands ${VAR} / ${VAR:-default} strictly inside scalar value nodes.
// String targets are tagged !!str so YAML metacharacters (newlines, #, :,
// quotes, anchors, booleans, numbers) can never alter document structure or types.
func expandNodeEnvVars(node *yaml.Node, targetType reflect.Type) {
	if node == nil {
		return
	}
	for targetType != nil && targetType.Kind() == reflect.Ptr {
		targetType = targetType.Elem()
	}

	switch node.Kind {
	case yaml.DocumentNode:
		for _, child := range node.Content {
			expandNodeEnvVars(child, targetType)
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			keyNode := node.Content[i]
			valNode := node.Content[i+1]
			var valType reflect.Type
			if targetType != nil {
				switch targetType.Kind() {
				case reflect.Struct:
					valType = yamlFieldType(targetType, keyNode.Value)
				case reflect.Map:
					valType = targetType.Elem()
				}
			}
			expandNodeEnvVars(valNode, valType)
		}
	case yaml.SequenceNode:
		var elemType reflect.Type
		if targetType != nil && (targetType.Kind() == reflect.Slice || targetType.Kind() == reflect.Array) {
			elemType = targetType.Elem()
		}
		for _, child := range node.Content {
			expandNodeEnvVars(child, elemType)
		}
	case yaml.ScalarNode:
		if !envVarPattern.MatchString(node.Value) {
			return
		}
		node.Value = expandEnvVars(node.Value)
		switch {
		case targetType == nil || targetType.Kind() == reflect.String || node.Style != 0:
			node.Tag = "!!str"
		case targetType == durationType:
			if node.Value == "" {
				node.Tag = "!!null"
			} else {
				node.Tag = "!!str"
			}
		default:
			// Unquoted non-string scalar (int, float, bool): keep default on
			// empty expansion, otherwise let yaml.v3 parse the single scalar.
			if node.Value == "" {
				node.Tag = "!!null"
			} else {
				node.Tag = ""
			}
		}
	}
}

func yamlFieldType(structType reflect.Type, key string) reflect.Type {
	for i := 0; i < structType.NumField(); i++ {
		f := structType.Field(i)
		if !f.IsExported() {
			continue
		}
		tag := f.Tag.Get("yaml")
		if tag == "-" {
			continue
		}
		parts := strings.Split(tag, ",")
		name := parts[0]
		inline := false
		for _, opt := range parts[1:] {
			if opt == "inline" {
				inline = true
			}
		}
		if inline {
			ft := f.Type
			for ft.Kind() == reflect.Ptr {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				if found := yamlFieldType(ft, key); found != nil {
					return found
				}
			}
			continue
		}
		if name == "" {
			name = strings.ToLower(f.Name)
		}
		if name == key {
			return f.Type
		}
	}
	return nil
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
	cfg, err := unmarshalConfig([]byte(content))
	if err != nil {
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
	cfg, err := unmarshalConfig([]byte(content))
	if err != nil {
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
