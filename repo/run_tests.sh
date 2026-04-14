#!/usr/bin/env bash
# run_tests.sh — single broad verification command for the portal.
# ALL tests run inside Docker containers so there is no dependency on the
# host machine having Go, Node, or any other toolchain installed.
#
# Usage:
#   ./run_tests.sh            — all tests (backend + frontend)
#   ./run_tests.sh --backend  — Go tests only
#   ./run_tests.sh --frontend — Vitest tests only
#   ./run_tests.sh --e2e      — Playwright E2E only
set -euo pipefail

ROOT="$(cd "$(dirname "$0")" && pwd)"
BACKEND_ONLY="${BACKEND_ONLY:-false}"
FRONTEND_ONLY="${FRONTEND_ONLY:-false}"
E2E_ONLY="${E2E_ONLY:-false}"
SKIP_E2E="${SKIP_E2E:-true}"  # skip E2E by default; set to false to run

# Parse flags
for arg in "$@"; do
  case "$arg" in
    --backend)  BACKEND_ONLY=true  ;;
    --frontend) FRONTEND_ONLY=true ;;
    --e2e)      E2E_ONLY=true; SKIP_E2E=false ;;
    --with-e2e) SKIP_E2E=false ;;
  esac
done

log()  { printf '\n\033[1;34m[run_tests] %s\033[0m\n' "$1"; }
pass() { printf '\033[1;32m  PASS: %s\033[0m\n' "$1"; }
fail() { printf '\033[1;31m  FAIL: %s\033[0m\n' "$1"; exit 1; }

cd "$ROOT"

EXIT_CODE=0

# ── Backend tests (Go inside Docker) ─────────────────────────────────────────
if [ "$FRONTEND_ONLY" != "true" ] && [ "$E2E_ONLY" != "true" ]; then
  log "Running Go backend tests inside Docker (golang:1.23-alpine)..."

  if docker run --rm \
    -v "$ROOT":/src \
    -w /src \
    golang:1.23-alpine \
    sh -c "apk add --no-cache git >/dev/null 2>&1 && go test -count=1 -timeout 120s ./tests/... ./internal/... ./cmd/..."; then
    pass "Backend tests"
  else
    fail "Backend tests"
  fi
fi

# ── Frontend tests (Node inside Docker) ──────────────────────────────────────
if [ "$BACKEND_ONLY" != "true" ] && [ "$E2E_ONLY" != "true" ]; then
  log "Running frontend unit and component tests inside Docker (node:20-alpine)..."

  if docker run --rm \
    -v "$ROOT":/src \
    -w /src \
    node:20-alpine \
    sh -c "npm ci --prefer-offline --silent 2>/dev/null; npx vitest run --reporter=verbose"; then
    pass "Frontend unit/component tests"
  else
    fail "Frontend unit/component tests"
  fi
fi

# ── E2E tests ─────────────────────────────────────────────────────────────────
if [ "$SKIP_E2E" != "true" ] || [ "$E2E_ONLY" = "true" ]; then
  log "Running Playwright E2E tests..."

  # E2E needs the full stack running, so we use the host network
  # and assume docker compose is already up.
  if docker run --rm \
    --network host \
    -v "$ROOT":/src \
    -w /src \
    -e BASE_URL="${BASE_URL:-http://localhost:3000}" \
    -e SKIP_WEBSERVER=1 \
    mcr.microsoft.com/playwright:v1.42.1-jammy \
    sh -c "npm ci --prefer-offline --silent 2>/dev/null; npx playwright test --reporter=line"; then
    pass "E2E tests"
  else
    fail "E2E tests"
  fi
else
  log "Skipping Playwright E2E (set SKIP_E2E=false or pass --e2e to run)"
fi

log "All tests completed successfully."
