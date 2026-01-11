# Task 01: Project Setup

## Objective
Initialize the Go project structure with dependencies and basic scaffolding.

## Subtasks

### 1.1 Initialize Go Module
- [x] Run `go mod init github.com/elqsar/pganalyzer`
- [x] Create directory structure:
  ```
  cmd/pganalyzer/
  internal/config/
  internal/postgres/
  internal/collector/
  internal/analyzer/
  internal/suggester/
  internal/storage/sqlite/
  internal/scheduler/
  internal/api/
  internal/web/templates/
  pkg/models/
  migrations/sqlite/
  configs/
  ```

### 1.2 Add Dependencies
- [x] `github.com/jackc/pgx/v5` - PostgreSQL driver
- [x] `modernc.org/sqlite` - Pure Go SQLite
- [x] `github.com/labstack/echo/v4` - HTTP router
- [x] `gopkg.in/yaml.v3` - YAML config parsing

### 1.3 Create Entry Point
- [x] Create `cmd/pganalyzer/main.go` with basic structure
- [x] Wire up signal handling for graceful shutdown
- [x] Add version/build info flags

### 1.4 Create Example Config
- [x] Create `configs/config.example.yaml` with all options documented

## Acceptance Criteria
- [x] `go build ./cmd/pganalyzer` succeeds
- [x] `go mod tidy` shows no missing dependencies
- [x] Directory structure matches tech design

## Completion Notes

**Completed:** 2026-01-10

**Files Created:**
- `cmd/pganalyzer/main.go` - Entry point with signal handling, version flags
- `configs/config.example.yaml` - Fully documented example configuration
- `Taskfile.yaml` - Common development commands
- `go.mod` / `go.sum` - Go module files

**Directory Structure:**
```
.
├── Taskfile.yaml
├── cmd/pganalyzer/
│   └── main.go
├── configs/
│   └── config.example.yaml
├── internal/
│   ├── analyzer/
│   ├── api/
│   ├── collector/
│   ├── config/
│   ├── postgres/
│   ├── scheduler/
│   ├── storage/sqlite/
│   ├── suggester/
│   └── web/templates/
├── migrations/sqlite/
└── pkg/models/
```
