# Implementation Tasks

This directory contains implementation tasks for PostgreSQL Analyzer (pganalyzer).

## Task Overview

| # | Task | Description | Dependencies |
|---|------|-------------|--------------|
| 01 | [Project Setup](01-project-setup.md) | Initialize Go module and project structure | - |
| 02 | [Configuration](02-configuration.md) | YAML config loading with env var expansion | 01 |
| 03 | [Storage (SQLite)](03-storage-sqlite.md) | SQLite storage layer with migrations | 01, 02 |
| 04 | [PostgreSQL Client](04-postgres-client.md) | PostgreSQL client for stats collection | 01, 02 |
| 05 | [Collectors](05-collectors.md) | Data collectors for metrics | 03, 04 |
| 06 | [Analyzer](06-analyzer.md) | Analysis logic for issue detection | 03, 05 |
| 07 | [Suggester](07-suggester.md) | Rules-based recommendation engine | 03, 06 |
| 08 | [Scheduler](08-scheduler.md) | Job scheduler for collection/analysis | 05, 06, 07 |
| 09 | [API Server](09-api-server.md) | REST API endpoints | 02, 03, 08 |
| 10 | [Web UI](10-web-ui.md) | Server-rendered HTML pages | 09 |
| 11 | [Production Readiness](11-production-readiness.md) | Docker, docs, testing | All |

## Dependency Graph

```
01 Project Setup
 │
 ├── 02 Configuration
 │    │
 │    ├── 03 Storage ─────────────────┐
 │    │    │                          │
 │    │    └── 05 Collectors ◄────────┤
 │    │         │                     │
 │    │         └── 06 Analyzer       │
 │    │              │                │
 │    │              └── 07 Suggester │
 │    │                   │           │
 │    │                   └── 08 Scheduler
 │    │                        │
 │    └── 04 PostgreSQL Client─┘
 │
 └── 09 API Server
      │
      └── 10 Web UI
           │
           └── 11 Production Readiness
```

## Suggested Implementation Order

**Phase 1: Foundation (Tasks 01-04)**
1. Project Setup
2. Configuration
3. Storage (SQLite)
4. PostgreSQL Client

**Phase 2: Data Pipeline (Tasks 05-08)**
5. Collectors
6. Analyzer
7. Suggester
8. Scheduler

**Phase 3: User Interface (Tasks 09-10)**
9. API Server
10. Web UI

**Phase 4: Deployment (Task 11)**
11. Production Readiness

## Progress Tracking

Update this section as tasks are completed:

- [x] 01 - Project Setup (completed 2026-01-10)
- [ ] 02 - Configuration
- [ ] 03 - Storage (SQLite)
- [ ] 04 - PostgreSQL Client
- [ ] 05 - Collectors
- [ ] 06 - Analyzer
- [ ] 07 - Suggester
- [ ] 08 - Scheduler
- [ ] 09 - API Server
- [ ] 10 - Web UI
- [ ] 11 - Production Readiness

## Quick Commands

Common development commands using [Task](https://taskfile.dev/):

```bash
# Build the application
task build

# Run the application
task run

# Run tests
task test

# Run tests with coverage
task test:coverage

# Format code
task fmt

# Run linters
task lint

# Clean build artifacts
task clean

# Show version info
task version

# Show all available tasks
task
```

## What Was Completed in Task 01

- Go module initialized (`github.com/user/pganalyzer`)
- Directory structure created matching tech design
- Dependencies added: pgx/v5, sqlite, echo/v4, yaml.v3
- Entry point created with signal handling and version flags
- Example configuration file created
- Taskfile.yaml created with common commands
