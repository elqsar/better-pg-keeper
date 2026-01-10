# Task 11: Production Readiness

## Objective
Prepare the application for production deployment with Docker, documentation, and testing.

## Subtasks

### 11.1 Create Dockerfile
Location: `Dockerfile`

- [ ] Multi-stage build (builder + runtime)
- [ ] Use `golang:1.22-alpine` for builder
- [ ] Use `alpine:3.19` for runtime
- [ ] CGO_ENABLED=0 for pure Go build
- [ ] Copy templates to runtime image
- [ ] Expose port 8080
- [ ] Set up volume for data directory
- [ ] Non-root user for security

### 11.2 Create Docker Compose
Location: `docker-compose.yaml`

- [ ] pganalyzer service
- [ ] Volume mounts for data and config
- [ ] Environment variable passthrough
- [ ] Health check configuration
- [ ] Optional: PostgreSQL service for testing

### 11.3 Create Taskfile
Location: `Taskfile.yaml`

- [ ] `task build` - build binary
- [ ] `task run` - run locally
- [ ] `task test` - run all tests
- [ ] `task lint` - run linter
- [ ] `task docker-build` - build Docker image
- [ ] `task docker-run` - run Docker container
- [ ] `task migrate` - run migrations manually

### 11.4 Write Integration Tests
Location: `tests/integration/`

- [ ] Test against real PostgreSQL with pg_stat_statements
- [ ] Test full collection cycle
- [ ] Test analysis and suggestion generation
- [ ] Test API endpoints end-to-end
- [ ] Use testcontainers or docker-compose

### 11.5 Write README
Location: `README.md`

- [ ] Project description
- [ ] Features list
- [ ] Quick start guide
- [ ] Prerequisites (pg_stat_statements setup)
- [ ] Configuration reference
- [ ] Docker deployment instructions
- [ ] Screenshots of UI
- [ ] Contributing guidelines

### 11.6 Create Example Configuration
Location: `configs/config.example.yaml`

- [ ] All options with comments
- [ ] Sensible defaults
- [ ] Environment variable placeholders
- [ ] Examples for common scenarios

### 11.7 Document PostgreSQL Setup
Location: `docs/postgresql-setup.md`

- [ ] Enable pg_stat_statements
- [ ] Create monitoring user with minimal privileges
- [ ] Grant necessary permissions
- [ ] Verify setup steps

### 11.8 Implement Logging
- [ ] Structured logging (JSON option)
- [ ] Log levels (debug, info, warn, error)
- [ ] Request logging middleware
- [ ] Collection/analysis logging

### 11.9 Implement Metrics (Optional)
- [ ] Prometheus endpoint `/metrics`
- [ ] Collection duration histogram
- [ ] Snapshot count counter
- [ ] Error counters

### 11.10 Security Review
- [ ] No secrets in logs
- [ ] Config file permissions warning
- [ ] SQL injection audit
- [ ] Input validation audit

## Acceptance Criteria
- [ ] Docker build succeeds
- [ ] Docker container runs correctly
- [ ] Integration tests pass
- [ ] Documentation is complete
- [ ] Example config works out of the box
