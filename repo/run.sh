#!/usr/bin/env bash
# run.sh — canonical first-boot startup wrapper.
# On first boot: generates runtime secrets, then starts docker compose.
# On subsequent boots: docker compose up --build alone is sufficient.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")" && pwd)"

log() { printf '[run.sh] %s\n' "$1" >&2; }

# Ensure runtime directory exists (used by bootstrap for local-mode testing)
mkdir -p "$ROOT/storage/private/runtime"
chmod 700 "$ROOT/storage/private/runtime"

log "Starting Workforce Learning & Procurement Reconciliation Portal..."
log "Using docker compose up --build"
log "Secrets are generated automatically on first boot via the bootstrap service."
log ""
log "Once started:"
log "  Web UI:  http://localhost:3000"
log "  API:     http://localhost:8080/api/health"
log ""
log "Bootstrap account passwords are written to the runtime_secrets Docker volume."
log "To view them: docker compose exec bootstrap cat /runtime/secrets/bootstrap_pw_admin.txt"

cd "$ROOT"
exec docker compose up --build "$@"
