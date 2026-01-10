# Task 02: Configuration

## Objective
Implement configuration loading with YAML parsing and environment variable expansion.

## Subtasks

### 2.1 Define Config Structs
Location: `internal/config/config.go`

- [x] Define `Config` struct matching schema:
  ```go
  type Config struct {
      Postgres   PostgresConfig
      Storage    StorageConfig
      Scheduler  SchedulerConfig
      Server     ServerConfig
      Thresholds ThresholdsConfig
  }
  ```
- [x] Add struct tags for YAML mapping
- [x] Add default values

### 2.2 Implement Config Loading
Location: `internal/config/loader.go`

- [x] Load YAML file from path (default: `./config.yaml`)
- [x] Support `PGANALYZER_CONFIG` env var override
- [x] Implement `${ENV_VAR}` expansion in string values
- [x] Merge defaults with loaded config

### 2.3 Implement Validation
Location: `internal/config/validation.go`

- [x] Validate required fields: `postgres.host`, `postgres.database`, `postgres.user`
- [x] Validate port range (1-65535)
- [x] Validate positive thresholds
- [x] Parse duration strings (`5m`, `1h`, etc.)
- [x] Return descriptive error messages

### 2.4 Write Tests
Location: `internal/config/config_test.go`

- [x] Test YAML parsing with valid config
- [x] Test env var expansion
- [x] Test validation errors
- [x] Test default values

## Acceptance Criteria
- [x] Config loads from YAML file
- [x] Environment variables are expanded
- [x] Validation catches invalid configs
- [x] Tests pass
