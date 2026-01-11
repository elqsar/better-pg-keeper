# Task 13: Tailwind CSS Migration

## Objective
Migrate the web UI from custom CSS to Tailwind CSS and improve aesthetics with modern design patterns.

## Subtasks

### 13.1 Set Up Tailwind Build System
Location: `internal/web/tailwind/`, `scripts/`, `Taskfile.yaml`

- [ ] Download Tailwind standalone CLI binary
- [ ] Create `internal/web/tailwind/tailwind.config.js` with custom theme
- [ ] Create `internal/web/tailwind/input.css` with Tailwind directives
- [ ] Create `scripts/build-css.sh` build script
- [ ] Add `css` and `css:watch` tasks to Taskfile.yaml
- [ ] Update `.gitignore` for Tailwind binary

### 13.2 Update Template Helpers
Location: `internal/web/templates.go`

- [ ] Add `cacheRatioTailwind()` function for color classes
- [ ] Add `severityTailwind()` function for badge classes
- [ ] Add `dict()` helper for passing values to sub-templates
- [ ] Keep existing helpers for backwards compatibility

### 13.3 Migrate Dashboard Template
Location: `internal/web/templates/dashboard.html`

- [ ] Convert stats grid to Tailwind classes
- [ ] Add hover effects and transitions to cards
- [ ] Update typography with proper hierarchy
- [ ] Style top queries table
- [ ] Style recent suggestions list
- [ ] Add empty state styling

### 13.4 Migrate Suggestions Template
Location: `internal/web/templates/suggestions.html`

- [ ] Convert filter dropdowns to Tailwind form styles
- [ ] Style severity summary cards
- [ ] Update suggestion cards with colored borders
- [ ] Style severity badges with `rounded-full`
- [ ] Add dismiss button styling

### 13.5 Migrate Queries Template
Location: `internal/web/templates/queries.html`

- [ ] Style filter controls
- [ ] Convert queries table with alternating rows
- [ ] Add sticky table headers
- [ ] Style pagination controls
- [ ] Update modal with backdrop blur
- [ ] Add loading spinner styling

### 13.6 Migrate Schema Template
Location: `internal/web/templates/schema.html`

- [ ] Style tab navigation with border indicator
- [ ] Convert Tables tab table
- [ ] Convert Indexes tab with status badges
- [ ] Convert Bloat tab with severity colors
- [ ] Style empty states

### 13.7 Migrate Query Detail Template
Location: `internal/web/templates/query_detail.html`

- [ ] Style statistics cards in two-column grid
- [ ] Style cache statistics table
- [ ] Format query text code block
- [ ] Style EXPLAIN plan output
- [ ] Add Generate/Refresh button styling

### 13.8 Remove Old CSS
Location: `internal/web/static/style.css`

- [ ] Verify all pages work with new Tailwind styles
- [ ] Remove old custom CSS (replaced by generated output)
- [ ] Run `task css` to generate final production CSS

### 13.9 Testing and Verification
- [ ] Visual comparison of all 5 pages
- [ ] Test responsive breakpoints (375px, 768px, 1024px)
- [ ] Test all interactive elements (modals, tabs, filters)
- [ ] Run `task test` to verify template tests pass
- [ ] Test in multiple browsers (Chrome, Firefox, Safari)

## Acceptance Criteria
- [ ] All pages render correctly with Tailwind
- [ ] `task css` generates production-ready CSS
- [ ] `task css:watch` works for development
- [ ] Responsive design works at all breakpoints
- [ ] All interactive elements function properly
- [ ] No visual regressions from original design
- [ ] Improved aesthetics with modern design patterns
- [ ] Tests pass
