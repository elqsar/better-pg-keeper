# Task 10: Web UI

## Objective
Implement server-rendered HTML pages using Go templates.

## Subtasks

### 10.1 Set Up Template Engine
Location: `internal/web/templates.go`

- [ ] Load templates from embedded filesystem
- [ ] Set up template functions (formatTime, formatBytes, etc.)
- [ ] Implement base layout template
- [ ] Configure Echo template renderer

### 10.2 Create Base Layout
Location: `internal/web/templates/layout.html`

- [ ] HTML5 structure
- [ ] Navigation header with links
- [ ] Main content area
- [ ] Footer with version info
- [ ] Include CSS (inline or linked)

### 10.3 Create Dashboard Page
Location: `internal/web/templates/dashboard.html`

- [ ] Route: `GET /`
- [ ] Overview cards:
  - Cache hit ratio (with color indicator)
  - Total queries tracked
  - Slow queries count
  - Active suggestions count
- [ ] Top queries table (5 rows)
- [ ] Recent suggestions list
- [ ] Last snapshot timestamp

### 10.4 Create Queries Page
Location: `internal/web/templates/queries.html`

- [ ] Route: `GET /queries`
- [ ] Sortable table with columns:
  - Query (truncated)
  - Calls
  - Mean time
  - Total time
  - Rows/call
- [ ] Pagination controls
- [ ] Click to expand query details
- [ ] EXPLAIN button for each query

### 10.5 Create Schema Page
Location: `internal/web/templates/schema.html`

- [ ] Route: `GET /schema`
- [ ] Tabs: Tables | Indexes | Bloat
- [ ] Tables tab:
  - Table name, size, rows, dead tuples
- [ ] Indexes tab:
  - Index name, table, scans, size, status
- [ ] Bloat tab:
  - Tables with high dead tuple ratio

### 10.6 Create Suggestions Page
Location: `internal/web/templates/suggestions.html`

- [ ] Route: `GET /suggestions`
- [ ] Filter by severity (critical, warning, info)
- [ ] Filter by status (active, dismissed)
- [ ] Card for each suggestion:
  - Severity badge
  - Title
  - Description
  - First/last seen dates
  - Dismiss button

### 10.7 Create Query Detail Modal/Page
Location: `internal/web/templates/query_detail.html`

- [ ] Full query text with syntax highlighting (optional)
- [ ] Execution statistics
- [ ] EXPLAIN plan (if available)
- [ ] Historical trend (calls/time over snapshots)

### 10.8 Implement CSS Styles
Location: `internal/web/static/style.css`

- [ ] Clean, minimal design
- [ ] Responsive layout
- [ ] Table styling
- [ ] Card components
- [ ] Severity colors (red, yellow, blue)
- [ ] Navigation styling

### 10.9 Implement Page Handlers
Location: `internal/api/handlers/pages.go`

- [ ] Dashboard handler - fetch and render
- [ ] Queries handler - fetch, paginate, render
- [ ] Schema handler - fetch and render
- [ ] Suggestions handler - fetch, filter, render

### 10.10 Write Tests
- [ ] Test template rendering
- [ ] Test page handlers return 200
- [ ] Test pagination in templates

## Acceptance Criteria
- [ ] All pages render correctly
- [ ] Navigation works
- [ ] Data displays properly
- [ ] Pages are responsive
- [ ] Tests pass
