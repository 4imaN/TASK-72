#!/usr/bin/env sh
# entrypoint-scheduler.sh — runs bootstrap, init_db, then starts the scheduler
set -eu

SECRETS_DIR="${SECRETS_DIR:-/runtime/secrets}"

log() { printf '[entrypoint-scheduler] %s\n' "$1" >&2; }

log "Running runtime bootstrap..."
SECRETS_DIR="$SECRETS_DIR" /app/infra/bootstrap/bootstrap-runtime.sh

log "Running database init..."
SECRETS_DIR="$SECRETS_DIR" /app/init_db.sh

log "Starting scheduler..."
exec /app/bin/scheduler "$@"
