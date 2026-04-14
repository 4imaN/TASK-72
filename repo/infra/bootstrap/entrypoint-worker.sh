#!/usr/bin/env sh
# entrypoint-worker.sh — runs bootstrap, init_db, then starts the background worker
set -eu

SECRETS_DIR="${SECRETS_DIR:-/runtime/secrets}"

log() { printf '[entrypoint-worker] %s\n' "$1" >&2; }

log "Running runtime bootstrap..."
SECRETS_DIR="$SECRETS_DIR" /app/infra/bootstrap/bootstrap-runtime.sh

log "Running database init..."
SECRETS_DIR="$SECRETS_DIR" /app/init_db.sh

log "Starting worker..."
exec /app/bin/worker "$@"
