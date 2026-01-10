# Task 03: SQLite Storage Layer

## Objective
Implement SQLite storage with migrations and CRUD operations for all entities.

## Subtasks

### 3.1 Create Migration System
Location: `internal/storage/sqlite/migrations.go`

- [ ] Embed SQL migrations using `embed.FS`
- [ ] Create `migrations` table to track applied migrations
- [ ] Implement `Migrate()` function to apply pending migrations
- [ ] Support rollback (optional for v1)

### 3.2 Create Schema Migrations
Location: `migrations/sqlite/`

- [ ] `001_create_instances.sql` - instances table
- [ ] `002_create_snapshots.sql` - snapshots table with index
- [ ] `003_create_query_stats.sql` - query_stats table with indexes
- [ ] `004_create_table_stats.sql` - table_stats table
- [ ] `005_create_index_stats.sql` - index_stats table
- [ ] `006_create_suggestions.sql` - suggestions table with indexes
- [ ] `007_create_explain_plans.sql` - explain_plans table

### 3.3 Implement Storage Interface
Location: `internal/storage/sqlite/storage.go`

- [ ] Define `Storage` interface (as per tech design section 4.5)
- [ ] Implement `NewStorage(dbPath string) (*SQLiteStorage, error)`
- [ ] Use connection pooling with `database/sql`

### 3.4 Implement Instance Operations
- [ ] `GetInstance(ctx, id) (*Instance, error)`
- [ ] `GetInstanceByName(ctx, name) (*Instance, error)`
- [ ] `CreateInstance(ctx, *Instance) (int64, error)`
- [ ] `GetOrCreateInstance(ctx, *Instance) (int64, error)`

### 3.5 Implement Snapshot Operations
- [ ] `CreateSnapshot(ctx, *Snapshot) (int64, error)`
- [ ] `GetLatestSnapshot(ctx, instanceID) (*Snapshot, error)`
- [ ] `GetSnapshotByID(ctx, id) (*Snapshot, error)`
- [ ] `ListSnapshots(ctx, instanceID, limit) ([]Snapshot, error)`

### 3.6 Implement Stats Operations
- [ ] `SaveQueryStats(ctx, snapshotID, []QueryStat) error`
- [ ] `GetQueryStats(ctx, snapshotID) ([]QueryStat, error)`
- [ ] `GetQueryStatsDelta(ctx, fromSnap, toSnap) ([]QueryStatDelta, error)`
- [ ] `SaveTableStats(ctx, snapshotID, []TableStat) error`
- [ ] `SaveIndexStats(ctx, snapshotID, []IndexStat) error`

### 3.7 Implement Suggestion Operations
- [ ] `UpsertSuggestion(ctx, *Suggestion) error`
- [ ] `GetActiveSuggestions(ctx, instanceID) ([]Suggestion, error)`
- [ ] `DismissSuggestion(ctx, id) error`
- [ ] `ResolveSuggestion(ctx, id) error`

### 3.8 Implement Maintenance Operations
- [ ] `PurgeOldSnapshots(ctx, retention) (int64, error)`
- [ ] Cascade delete related stats when purging snapshots

### 3.9 Write Tests
- [ ] Test migrations apply correctly
- [ ] Test CRUD operations for each entity
- [ ] Test delta calculation logic
- [ ] Test purge with cascading deletes

## Acceptance Criteria
- [ ] Migrations create all tables with correct schema
- [ ] All CRUD operations work correctly
- [ ] Delta calculation handles stats resets
- [ ] Purge correctly removes old data
- [ ] Tests pass
