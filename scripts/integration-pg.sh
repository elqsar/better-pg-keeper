#!/bin/bash
# Runs the integration suite against a throwaway PostgreSQL container, started
# with the settings the tests check for (see the comments in tests/integration).
#
# Usage: scripts/integration-pg.sh [major-version]   (default 17)
# CONTAINER_ENGINE picks podman (default) or docker, which CI uses.
set -euo pipefail

PG_VERSION="${1:-17}"
ENGINE="${CONTAINER_ENGINE:-podman}"
NAME="pgk-ci-${PG_VERSION}"
PORT="155${PG_VERSION}"
PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"

cleanup() { "$ENGINE" rm -f "$NAME" >/dev/null 2>&1 || true; }
trap cleanup EXIT
cleanup

echo "Starting PostgreSQL ${PG_VERSION} as ${NAME} on port ${PORT}..."
"$ENGINE" run -d --rm --name "$NAME" -p "${PORT}:5432" \
    -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=testdb \
    "docker.io/library/postgres:${PG_VERSION}" \
    -c shared_preload_libraries=pg_stat_statements \
    -c pg_stat_statements.max=100 \
    -c max_prepared_transactions=10 >/dev/null

# hypopg needs no preload, so it can be installed while the server starts. The
# official images carry the PGDG apt repository.
"$ENGINE" exec -u root "$NAME" sh -c \
    "export DEBIAN_FRONTEND=noninteractive && apt-get update -qq && apt-get install -y -qq postgresql-${PG_VERSION}-hypopg" >/dev/null

# The entrypoint's init server listens only on the socket, so wait on TCP.
for _ in $(seq 60); do
    if "$ENGINE" exec "$NAME" pg_isready -q -h 127.0.0.1 -U postgres; then
        break
    fi
    sleep 1
done
"$ENGINE" exec "$NAME" pg_isready -h 127.0.0.1 -U postgres
"$ENGINE" exec "$NAME" psql -q -h 127.0.0.1 -U postgres -d testdb \
    -c 'CREATE EXTENSION IF NOT EXISTS pg_stat_statements'

cd "$PROJECT_ROOT"
POSTGRES_HOST=localhost POSTGRES_PORT="$PORT" POSTGRES_USER=postgres \
    POSTGRES_PASSWORD=postgres POSTGRES_DATABASE=testdb \
    go test -count=1 -tags=integration ./tests/integration/...
