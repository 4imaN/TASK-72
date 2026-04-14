#!/usr/bin/env bash
# run_tests.sh — single broad verification command for the portal.
# Runs backend Go tests, frontend Vitest tests, and (optionally) Playwright E2E.
# Usage:
#   ./run_tests.sh            — all tests
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

# ── Backend tests ─────────────────────────────────────────────────────────────
if [ "$FRONTEND_ONLY" != "true" ] && [ "$E2E_ONLY" != "true" ]; then
  log "Running Go backend tests..."

  # Ensure go.sum is up to date
  go mod tidy 2>/dev/null || true

  # Run all backend tests with race detector
  if go test -race -count=1 -timeout 120s ./tests/... ./internal/... ./cmd/...; then
    pass "Backend tests"
  else
    fail "Backend tests"
  fi

  # Run with coverage
  log "Measuring backend test coverage..."
  COVERAGE_FILE="/tmp/portal_coverage.out"
  go test -coverprofile="$COVERAGE_FILE" -covermode=atomic ./tests/... ./internal/... ./cmd/... 2>/dev/null || true
  if [ -f "$COVERAGE_FILE" ]; then
    go tool cover -func="$COVERAGE_FILE" | grep total: | awk '{print "  Coverage: " $3}'
  fi
fi

# ── Frontend tests ────────────────────────────────────────────────────────────
if [ "$BACKEND_ONLY" != "true" ] && [ "$E2E_ONLY" != "true" ]; then
  log "Running frontend unit and component tests (Vitest)..."

  # Install dependencies if node_modules is missing
  if [ ! -d "$ROOT/node_modules" ]; then
    log "Installing npm dependencies..."
    npm ci --prefer-offline
  fi

  if npm test -- --reporter=verbose; then
    pass "Frontend unit/component tests"
  else
    fail "Frontend unit/component tests"
  fi
fi

# ── E2E tests ─────────────────────────────────────────────────────────────────
if [ "$SKIP_E2E" != "true" ] || [ "$E2E_ONLY" = "true" ]; then
  log "Running Playwright E2E tests..."

  # Ensure browsers are installed
  npx playwright install --with-deps chromium 2>/dev/null || true

  BASE_URL="${BASE_URL:-http://localhost:3000}" SKIP_WEBSERVER=1 \
    npx playwright test --reporter=line
  pass "E2E tests"
else
  log "Skipping Playwright E2E (set SKIP_E2E=false or pass --e2e to run)"
fi

log "All tests completed successfully."
