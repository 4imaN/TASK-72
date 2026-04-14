// tests/unit/logging_test.go — tests for the structured logger.
package unit_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"portal/internal/platform/logging"
)

func TestLoggerWritesJSON(t *testing.T) {
	var buf bytes.Buffer
	log := logging.New(&buf, logging.INFO, "test")

	log.Info("hello world")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("expected valid JSON log line, got: %s — err: %v", buf.String(), err)
	}

	if entry["level"] != "info" {
		t.Errorf("expected level=info, got %v", entry["level"])
	}
	if entry["msg"] != "hello world" {
		t.Errorf("expected msg='hello world', got %v", entry["msg"])
	}
	if entry["category"] != "test" {
		t.Errorf("expected category=test, got %v", entry["category"])
	}
	if _, ok := entry["ts"]; !ok {
		t.Error("expected ts field")
	}
}

func TestLoggerDebugFiltered(t *testing.T) {
	var buf bytes.Buffer
	log := logging.New(&buf, logging.INFO, "test")

	log.Debug("this should be filtered")

	if buf.Len() > 0 {
		t.Errorf("expected no output for DEBUG when minLevel=INFO, got: %s", buf.String())
	}
}

func TestLoggerWithFields(t *testing.T) {
	var buf bytes.Buffer
	log := logging.New(&buf, logging.INFO, "test")

	log.Info("request", map[string]any{"method": "GET", "path": "/api/health", "status": 200})

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("expected valid JSON: %v", err)
	}

	if entry["method"] != "GET" {
		t.Errorf("expected method=GET, got %v", entry["method"])
	}
	if entry["path"] != "/api/health" {
		t.Errorf("expected path=/api/health, got %v", entry["path"])
	}
}

func TestLoggerError(t *testing.T) {
	var buf bytes.Buffer
	log := logging.New(&buf, logging.DEBUG, "test")

	log.Error("something failed", map[string]any{"err": "disk full"})

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("expected valid JSON: %v", err)
	}
	if entry["level"] != "error" {
		t.Errorf("expected level=error, got %v", entry["level"])
	}
}

func TestLoggerWarn(t *testing.T) {
	var buf bytes.Buffer
	log := logging.New(&buf, logging.DEBUG, "test")

	log.Warn("slow query", map[string]any{"duration_ms": 1500})

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("expected valid JSON: %v", err)
	}
	if entry["level"] != "warn" {
		t.Errorf("expected level=warn, got %v", entry["level"])
	}
}
