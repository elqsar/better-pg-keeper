# Task 09: API Server

## Objective
Implement HTTP server with REST API endpoints and authentication.

## Subtasks

### 9.1 Set Up Echo Server
Location: `internal/api/server.go`

- [ ] Create Echo instance
- [ ] Configure middleware (logging, recovery, CORS)
- [ ] Set up route groups
- [ ] Implement graceful shutdown

### 9.2 Implement Basic Auth Middleware
Location: `internal/api/middleware/auth.go`

- [ ] Parse Authorization header
- [ ] Validate against config credentials
- [ ] Skip auth for health endpoint
- [ ] Return 401 on failure

### 9.3 Implement Health Endpoint
Location: `internal/api/handlers/health.go`

- [ ] `GET /health`
- [ ] Check PostgreSQL connectivity
- [ ] Return last snapshot time
- [ ] Response:
  ```json
  {"status": "ok", "pg_connected": true, "last_snapshot": "..."}
  ```

### 9.4 Implement Dashboard API
Location: `internal/api/handlers/dashboard.go`

- [ ] `GET /api/v1/dashboard`
- [ ] Return:
  - Cache hit ratio
  - Total unique queries
  - Slow queries count
  - Active suggestions count
  - Top 5 queries by total time
  - Recent suggestions

### 9.5 Implement Queries API
Location: `internal/api/handlers/queries.go`

- [ ] `GET /api/v1/queries`
  - Query params: sort, order, limit, offset
  - Sort options: calls, mean_time, total_time, rows
  - Return paginated query list

- [ ] `GET /api/v1/queries/top`
  - Query params: metric (calls, time, rows), limit
  - Return top N queries by metric

- [ ] `POST /api/v1/queries/:id/explain`
  - Run EXPLAIN on query
  - Store result in explain_plans
  - Return plan text and parsed info

### 9.6 Implement Schema API
Location: `internal/api/handlers/schema.go`

- [ ] `GET /api/v1/schema/tables`
  - Return table stats with sizes

- [ ] `GET /api/v1/schema/indexes`
  - Return index stats
  - Include usage and size

- [ ] `GET /api/v1/schema/bloat`
  - Return tables with significant bloat

### 9.7 Implement Suggestions API
Location: `internal/api/handlers/suggestions.go`

- [ ] `GET /api/v1/suggestions`
  - Query params: status (active, dismissed), severity
  - Return filtered suggestions

- [ ] `POST /api/v1/suggestions/:id/dismiss`
  - Mark suggestion as dismissed
  - Record dismissal time

### 9.8 Implement Snapshots API
Location: `internal/api/handlers/snapshots.go`

- [ ] `POST /api/v1/snapshots`
  - Trigger manual snapshot collection
  - Return snapshot ID and status

- [ ] `GET /api/v1/snapshots`
  - List recent snapshots
  - Include collection duration

### 9.9 Implement Error Handling
Location: `internal/api/errors.go`

- [ ] Define error response format
- [ ] Map internal errors to HTTP status codes
- [ ] Log errors with context

### 9.10 Write Tests
- [ ] Test auth middleware
- [ ] Test each endpoint with valid data
- [ ] Test error responses
- [ ] Test pagination

## Acceptance Criteria
- [ ] All endpoints return correct responses
- [ ] Authentication works correctly
- [ ] Errors are properly formatted
- [ ] Pagination works
- [ ] Tests pass
