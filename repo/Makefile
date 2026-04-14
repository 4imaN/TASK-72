.PHONY: build test test-fast test-integration test-e2e test-all lint

# Compile all Go packages.
build:
	go build ./...

# Fast suite: in-memory fakes + source-level assertions. No external deps.
test-fast:
	go test ./tests/api/... ./tests/security/... ./tests/unit/... ./cmd/...

# Integration suite: real PostgreSQL + real handler chain. Requires
# INTEGRATION_DATABASE_URL pointing at a throwaway database. Tests skip
# cleanly when the env var is empty.
#
#   make test-integration INTEGRATION_DATABASE_URL=postgres://user:pw@localhost/portal_it?sslmode=disable
test-integration:
	@if [ -z "$$INTEGRATION_DATABASE_URL" ]; then \
		echo "Set INTEGRATION_DATABASE_URL to run integration tests."; \
		echo "Tests will skip without it."; \
	fi
	go test ./tests/integration/... -v -count=1

# Playwright end-to-end smoke suite.
test-e2e:
	npx playwright test --reporter=line

# Everything.
test-all: test-fast test-integration test-e2e

test: test-fast

# Static checks.
lint:
	go vet ./...
