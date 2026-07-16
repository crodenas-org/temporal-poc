#!/usr/bin/env bash
# One-shot Temporal schema setup, run by the `temporal-schema` container using
# the stock admin-tools image. This is the schema-migration half of what the old
# auto-setup image did on boot (see AUTHZ.md §5) — now an explicit, one-shot step.
#
# Idempotent: create-database tolerates "already exists"; setup/update-schema are
# safe to re-run.
set -euo pipefail

PLUGIN="${SQL_PLUGIN:-postgres12}"
HOST="${SQL_HOST:-postgresql}"
PORT="${SQL_PORT:-5432}"
DB_USER="${SQL_USER:-temporal}"
DB_PWD="${SQL_PASSWORD:-temporal}"
SCHEMA_BASE="${SCHEMA_BASE:-/etc/temporal/schema/postgresql/v12}"

sql() {
  temporal-sql-tool --plugin "$PLUGIN" --ep "$HOST" -p "$PORT" -u "$DB_USER" --pw "$DB_PWD" "$@"
}

setup_db() {
  local db="$1" schema_dir="$2"
  echo ">> ensuring database '$db'"
  sql --db "$db" create-database || true
  echo ">> setup + update schema for '$db' from $schema_dir"
  sql --db "$db" setup-schema -v 0.0
  sql --db "$db" update-schema -d "$schema_dir"
}

setup_db "temporal"            "$SCHEMA_BASE/temporal/versioned"
setup_db "temporal_visibility" "$SCHEMA_BASE/visibility/versioned"
echo ">> schema setup complete"
