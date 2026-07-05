#!/usr/bin/env bash
# =============================================================================
# baasparse — database install / upgrade script
# =============================================================================
# Applies .db/schema.sql (the baseline 0001_initial schema for all baasparse
# state tables) to a PostgreSQL database via psql. Idempotent — safe to re-run.
#
# Make executable once:   chmod +x .db/install.sh
# Run:                    ./.db/install.sh
#
# Connection is taken from standard libpq environment variables, with defaults:
#   PGHOST      (default: localhost)
#   PGPORT      (default: 5432)
#   PGUSER      (default: postgres)
#   PGPASSWORD  (default: empty — rely on ~/.pgpass, peer/trust auth, etc.)
#   PGDATABASE  (default: baasparse)   <- the database the schema is installed into
#
# Alternatively, set DATABASE_URL to a full libpq connection string/URI and it
# takes precedence over the individual PG* variables:
#   DATABASE_URL="postgres://user:pass@host:5432/baasparse" ./.db/install.sh
#
# The script creates the target database if it does not already exist, then runs
# schema.sql with ON_ERROR_STOP so any failure aborts loudly.
# =============================================================================

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCHEMA_FILE="${SCRIPT_DIR}/schema.sql"

if [[ ! -f "${SCHEMA_FILE}" ]]; then
  echo "ERROR: schema file not found: ${SCHEMA_FILE}" >&2
  exit 1
fi

# ---------------------------------------------------------------------------
# Path A: a single DATABASE_URL was provided — use it verbatim.
# ---------------------------------------------------------------------------
if [[ -n "${DATABASE_URL:-}" ]]; then
  echo ">> Using DATABASE_URL connection string."
  echo ">> Applying schema: ${SCHEMA_FILE}"
  # Note: with DATABASE_URL we assume the database already exists (the URL names
  # it). Creating it would require parsing the URL; instead we let psql connect
  # and fail clearly if the database is missing.
  psql "${DATABASE_URL}" -v ON_ERROR_STOP=1 -f "${SCHEMA_FILE}"
  echo ">> Done. baasparse schema applied (idempotent)."
  exit 0
fi

# ---------------------------------------------------------------------------
# Path B: individual PG* variables (with defaults).
# ---------------------------------------------------------------------------
export PGHOST="${PGHOST:-localhost}"
export PGPORT="${PGPORT:-5432}"
export PGUSER="${PGUSER:-postgres}"
export PGDATABASE="${PGDATABASE:-baasparseDB}"
# PGPASSWORD is exported only if set, so ~/.pgpass / peer / trust auth still work.
if [[ -n "${PGPASSWORD:-}" ]]; then export PGPASSWORD; fi

TARGET_DB="${PGDATABASE}"

echo ">> Connection: host=${PGHOST} port=${PGPORT} user=${PGUSER} database=${TARGET_DB}"

# Create the target database if it does not already exist. We connect to the
# maintenance database 'postgres' to check/create, so unset PGDATABASE for these
# admin commands.
echo ">> Checking whether database '${TARGET_DB}' exists..."
DB_EXISTS="$(PGDATABASE=postgres psql -tAc \
  "SELECT 1 FROM pg_database WHERE datname = '${TARGET_DB}';" || true)"

if [[ "${DB_EXISTS}" == "1" ]]; then
  echo ">> Database '${TARGET_DB}' already exists — reusing it."
else
  echo ">> Database '${TARGET_DB}' not found — creating it."
  PGDATABASE=postgres psql -v ON_ERROR_STOP=1 -c "CREATE DATABASE \"${TARGET_DB}\";"
fi

echo ">> Applying schema: ${SCHEMA_FILE}"
psql -v ON_ERROR_STOP=1 -f "${SCHEMA_FILE}"

echo ">> Done. baasparse schema applied to '${TARGET_DB}' (idempotent — safe to re-run)."
