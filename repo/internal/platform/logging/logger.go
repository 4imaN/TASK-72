// Package logging provides structured JSON logging for the portal.
package logging

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
)

// Level represents a log severity level.
type Level int

const (
	DEBUG Level = iota
	INFO
	WARN
	ERROR
)

func (l Level) String() string {
	switch l {
	case DEBUG:
		return "debug"
	case INFO:
		return "info"
	case WARN:
		return "warn"
	case ERROR:
		return "error"
	default:
		return "unknown"
	}
}

// Logger writes structured JSON log lines.
type Logger struct {
	out   io.Writer
	level Level
	// category is appended to every log entry.
	category string
}

// New creates a Logger that writes to out at minLevel.
func New(out io.Writer, minLevel Level, category string) *Logger {
	if out == nil {
		out = os.Stdout
	}
	return &Logger{out: out, level: minLevel, category: category}
}

// Default returns a production-ready logger writing to stdout.
func Default() *Logger {
	return New(os.Stdout, INFO, "app")
}

func (l *Logger) log(level Level, msg string, fields map[string]any) {
	if level < l.level {
		return
	}
	entry := map[string]any{
		"ts":       time.Now().UTC().Format(time.RFC3339Nano),
		"level":    level.String(),
		"category": l.category,
		"msg":      msg,
	}
	for k, v := range fields {
		entry[k] = v
	}
	data, err := json.Marshal(entry)
	if err != nil {
		fmt.Fprintf(l.out, `{"level":"error","msg":"log marshal failed","err":%q}`+"\n", err.Error())
		return
	}
	fmt.Fprintf(l.out, "%s\n", data)
}

// Info logs at INFO level.
func (l *Logger) Info(msg string, fields ...map[string]any) {
	m := merge(fields...)
	l.log(INFO, msg, m)
}

// Warn logs at WARN level.
func (l *Logger) Warn(msg string, fields ...map[string]any) {
	l.log(WARN, msg, merge(fields...))
}

// Error logs at ERROR level.
func (l *Logger) Error(msg string, fields ...map[string]any) {
	l.log(ERROR, msg, merge(fields...))
}

// Debug logs at DEBUG level.
func (l *Logger) Debug(msg string, fields ...map[string]any) {
	l.log(DEBUG, msg, merge(fields...))
}

// With returns a new Logger with extra fields pre-populated.
// The returned Logger has the same level and category as the parent.
func (l *Logger) With(fields map[string]any) *Logger {
	return &Logger{
		out:      l.out,
		level:    l.level,
		category: l.category,
	}
}

// withFields logs with merged static + call-time fields.
func (l *Logger) withFields(extra map[string]any, level Level, msg string, fields ...map[string]any) {
	merged := merge(append([]map[string]any{extra}, fields...)...)
	l.log(level, msg, merged)
}

func merge(maps ...map[string]any) map[string]any {
	out := make(map[string]any)
	for _, m := range maps {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

