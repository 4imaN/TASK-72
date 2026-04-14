#!/usr/bin/env bash
# init_db.sh — project-standard database preparation path.
# Idempotent: safe to run multiple times.
# Called by each service entrypoint before starting.
set -euo pipefail

SECRETS_DIR="${SECRETS_DIR:-/runtime/secrets}"
DB_HOST="${DB_HOST:-postgres}"
DB_PORT="${DB_PORT:-5432}"
DB_SUPERUSER="${DB_SUPERUSER:-postgres}"
APP_DB="${APP_DB:-portal}"
APP_USER="${APP_USER:-portal_app}"
MIGRATIONS_DIR="${MIGRATIONS_DIR:-/app/migrations}"
SEEDS_DIR="${SEEDS_DIR:-/app/seeds}"
# Write the init marker to a writable location (/app/storage), not the
# read-only secrets volume.
MARKER_FILE="${STORAGE_PATH:-/app/storage}/db_init_complete.marker"

log()  { printf '[init_db] %s\n' "$1" >&2; }
fail() { printf '[init_db] ERROR: %s\n' "$1" >&2; exit 1; }

# ── Read secrets ──────────────────────────────────────────────────────────────
[ -f "$SECRETS_DIR/db_superpassword.txt" ] || fail "db_superpassword.txt not found in $SECRETS_DIR"
[ -f "$SECRETS_DIR/db_apppassword.txt"   ] || fail "db_apppassword.txt not found in $SECRETS_DIR"

PGPASSWORD=$(cat "$SECRETS_DIR/db_superpassword.txt")
export PGPASSWORD
APP_PASSWORD=$(cat "$SECRETS_DIR/db_apppassword.txt")

# ── Wait for PostgreSQL ───────────────────────────────────────────────────────
log "Waiting for PostgreSQL at $DB_HOST:$DB_PORT..."
for i in $(seq 1 30); do
  if pg_isready -h "$DB_HOST" -p "$DB_PORT" -U "$DB_SUPERUSER" -q; then
    log "PostgreSQL is ready."
    break
  fi
  log "  attempt $i/30 — sleeping 2s"
  sleep 2
  if [ "$i" -eq 30 ]; then
    fail "PostgreSQL did not become ready in time."
  fi
done

psql_super() {
  psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_SUPERUSER" "$@"
}

psql_app() {
  PGPASSWORD="$APP_PASSWORD" psql -h "$DB_HOST" -p "$DB_PORT" -U "$APP_USER" -d "$APP_DB" "$@"
}

# ── Create app user if missing ────────────────────────────────────────────────
USER_EXISTS=$(psql_super -tAc "SELECT 1 FROM pg_roles WHERE rolname='$APP_USER'" postgres)
if [ "$USER_EXISTS" != "1" ]; then
  log "Creating app user $APP_USER..."
  psql_super -c "CREATE ROLE $APP_USER WITH LOGIN PASSWORD '$APP_PASSWORD';" postgres
else
  log "App user $APP_USER already exists."
  # Ensure password is current (idempotent update)
  psql_super -c "ALTER ROLE $APP_USER WITH PASSWORD '$APP_PASSWORD';" postgres
fi

# ── Create database if missing ────────────────────────────────────────────────
DB_EXISTS=$(psql_super -tAc "SELECT 1 FROM pg_database WHERE datname='$APP_DB'" postgres)
if [ "$DB_EXISTS" != "1" ]; then
  log "Creating database $APP_DB..."
  psql_super -c "CREATE DATABASE $APP_DB OWNER $APP_USER ENCODING 'UTF8' LC_COLLATE 'en_US.utf8' LC_CTYPE 'en_US.utf8' TEMPLATE template0;" postgres
else
  log "Database $APP_DB already exists."
fi

# ── Grant privileges ──────────────────────────────────────────────────────────
psql_super -d "$APP_DB" -c "GRANT ALL PRIVILEGES ON DATABASE $APP_DB TO $APP_USER;" postgres 2>/dev/null || true
psql_super -d "$APP_DB" -c "ALTER DATABASE $APP_DB OWNER TO $APP_USER;" postgres 2>/dev/null || true

# ── Enable extensions ─────────────────────────────────────────────────────────
log "Enabling PostgreSQL extensions..."
psql_super -d "$APP_DB" -c "CREATE EXTENSION IF NOT EXISTS pg_trgm;"    >/dev/null
psql_super -d "$APP_DB" -c "CREATE EXTENSION IF NOT EXISTS unaccent;"   >/dev/null
psql_super -d "$APP_DB" -c "CREATE EXTENSION IF NOT EXISTS pgcrypto;"   >/dev/null
psql_super -d "$APP_DB" -c "CREATE EXTENSION IF NOT EXISTS btree_gin;"  >/dev/null

# Grant schema usage to app user
psql_super -d "$APP_DB" -c "GRANT USAGE ON SCHEMA public TO $APP_USER;" >/dev/null
psql_super -d "$APP_DB" -c "GRANT CREATE ON SCHEMA public TO $APP_USER;" >/dev/null

# ── Run migrations ────────────────────────────────────────────────────────────
log "Running migrations from $MIGRATIONS_DIR..."
APPLIED=0
for migration_file in $(ls "$MIGRATIONS_DIR"/*.sql 2>/dev/null | sort); do
  filename=$(basename "$migration_file")
  # Check if migration was already applied
  APPLIED_CHECK=$(PGPASSWORD="$APP_PASSWORD" psql -h "$DB_HOST" -p "$DB_PORT" -U "$APP_USER" -d "$APP_DB" \
    -tAc "SELECT 1 FROM schema_migrations WHERE filename='$filename'" 2>/dev/null || echo "")
  if [ "$APPLIED_CHECK" = "1" ]; then
    log "  Migration $filename already applied — skipping"
    continue
  fi
  log "  Applying migration: $filename"
  PGPASSWORD="$APP_PASSWORD" psql -h "$DB_HOST" -p "$DB_PORT" -U "$APP_USER" -d "$APP_DB" \
    -f "$migration_file" -q
  PGPASSWORD="$APP_PASSWORD" psql -h "$DB_HOST" -p "$DB_PORT" -U "$APP_USER" -d "$APP_DB" \
    -c "INSERT INTO schema_migrations(filename, applied_at) VALUES('$filename', NOW()) ON CONFLICT DO NOTHING;"
  log "  Migration $filename applied."
  APPLIED=$((APPLIED + 1))
done
log "Migrations complete. Applied $APPLIED new migration(s)."

# ── Run seeds ────────────────────────────────────────────────────────────────
log "Running seeds from $SEEDS_DIR..."
SEED_APPLIED=0
for seed_file in $(ls "$SEEDS_DIR"/*.sql 2>/dev/null | sort); do
  filename=$(basename "$seed_file")
  SEED_CHECK=$(PGPASSWORD="$APP_PASSWORD" psql -h "$DB_HOST" -p "$DB_PORT" -U "$APP_USER" -d "$APP_DB" \
    -tAc "SELECT 1 FROM seed_runs WHERE filename='$filename'" 2>/dev/null || echo "")
  if [ "$SEED_CHECK" = "1" ]; then
    log "  Seed $filename already applied — skipping"
    continue
  fi
  log "  Applying seed: $filename"
  PGPASSWORD="$APP_PASSWORD" psql -h "$DB_HOST" -p "$DB_PORT" -U "$APP_USER" -d "$APP_DB" \
    -f "$seed_file" -q
  PGPASSWORD="$APP_PASSWORD" psql -h "$DB_HOST" -p "$DB_PORT" -U "$APP_USER" -d "$APP_DB" \
    -c "INSERT INTO seed_runs(filename, applied_at) VALUES('$filename', NOW()) ON CONFLICT DO NOTHING;"
  log "  Seed $filename applied."
  SEED_APPLIED=$((SEED_APPLIED + 1))
done
log "Seeds complete. Applied $SEED_APPLIED new seed(s)."

# ── Inject bootstrap account passwords from secrets ───────────────────────────
log "Updating bootstrap account passwords from secrets..."
for role in learner procurement approver finance moderator admin; do
  f="$SECRETS_DIR/bootstrap_pw_${role}.txt"
  if [ -f "$f" ]; then
    pw=$(cat "$f")
    # Hash with pgcrypto (bcrypt cost 12) and update if account still has temporary marker
    PGPASSWORD="$APP_PASSWORD" psql -h "$DB_HOST" -p "$DB_PORT" -U "$APP_USER" -d "$APP_DB" \
      -c "UPDATE users SET password_hash = crypt('$pw', gen_salt('bf', 12)) WHERE username = 'bootstrap_${role}' AND force_password_reset = TRUE AND password_hash = 'BOOTSTRAP_PLACEHOLDER';" \
      -q 2>/dev/null || true
  fi
done
log "Bootstrap account passwords updated."

# ── Write success marker ──────────────────────────────────────────────────────
date -u +"%Y-%m-%dT%H:%M:%SZ" > "$MARKER_FILE"
log "Database initialization complete. Marker written to $MARKER_FILE."
