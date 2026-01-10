# Task 02: Configuration

## Objective
Implement configuration loading with YAML parsing and environment variable expansion.

## Subtasks

### 2.1 Define Config Structs
Location: `internal/config/config.go`

- [ ] Define `Config` struct matching schema:
  ```go
  type Config struct {
      Postgres   PostgresConfig
      Storage    StorageConfig
      Scheduler  SchedulerConfig
      Server     ServerConfig
      Thresholds ThresholdsConfig
  }
  ```
- [ ] Add struct tags for YAML mapping
- [ ] Add default values

### 2.2 Implement Config Loading
Location: `internal/config/loader.go`

- [ ] Load YAML file from path (default: `./config.yaml`)
- [ ] Support `PGANALYZER_CONFIG` env var override
- [ ] Implement `${ENV_VAR}` expansion in string values
- [ ] Merge defaults with loaded config

### 2.3 Implement Validation
Location: `internal/config/validation.go`

- [ ] Validate required fields: `postgres.host`, `postgres.database`, `postgres.user`
- [ ] Validate port range (1-65535)
- [ ] Validate positive thresholds
- [ ] Parse duration strings (`5m`, `1h`, etc.)
- [ ] Return descriptive error messages

### 2.4 Write Tests
Location: `internal/config/config_test.go`

- [ ] Test YAML parsing with valid config
- [ ] Test env var expansion
- [ ] Test validation errors
- [ ] Test default values

## Acceptance Criteria
- [ ] Config loads from YAML file
- [ ] Environment variables are expanded
- [ ] Validation catches invalid configs
- [ ] Tests pass
