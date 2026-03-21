package control

import (
	"context"
	"fmt"
	"log/slog"
	"testing"
	"time"
)

func entry(line string) LogEntry {
	return LogEntry{Level: slog.LevelInfo, Line: line}
}

func TestLogBufferStoresLines(t *testing.T) {
	buf := NewLogBuffer(100)

	buf.Append(entry("line 1"))
	buf.Append(entry("line 2"))
	buf.Append(entry("line 3"))

	lines := buf.Last(10, slog.LevelDebug)
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(lines))
	}
	if lines[0] != "line 1" || lines[1] != "line 2" || lines[2] != "line 3" {
		t.Errorf("lines = %v", lines)
	}
}

func TestLogBufferWrapsAround(t *testing.T) {
	buf := NewLogBuffer(3)

	buf.Append(entry("a"))
	buf.Append(entry("b"))
	buf.Append(entry("c"))
	buf.Append(entry("d"))

	lines := buf.Last(10, slog.LevelDebug)
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(lines))
	}
	if lines[0] != "b" || lines[1] != "c" || lines[2] != "d" {
		t.Errorf("lines = %v, want [b c d]", lines)
	}
}

func TestLogBufferLastN(t *testing.T) {
	buf := NewLogBuffer(100)
	for i := 0; i < 20; i++ {
		buf.Append(entry(fmt.Sprintf("line %d", i)))
	}

	lines := buf.Last(5, slog.LevelDebug)
	if len(lines) != 5 {
		t.Fatalf("got %d lines, want 5", len(lines))
	}
	if lines[0] != "line 15" || lines[4] != "line 19" {
		t.Errorf("lines = %v", lines)
	}
}

func TestLogBufferLastMoreThanAvailable(t *testing.T) {
	buf := NewLogBuffer(100)
	buf.Append(entry("only"))

	lines := buf.Last(10, slog.LevelDebug)
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
}

func TestLogBufferLastFiltersByLevel(t *testing.T) {
	buf := NewLogBuffer(100)

	buf.Append(LogEntry{Level: slog.LevelDebug, Line: "debug"})
	buf.Append(LogEntry{Level: slog.LevelInfo, Line: "info"})
	buf.Append(LogEntry{Level: slog.LevelWarn, Line: "warn"})
	buf.Append(LogEntry{Level: slog.LevelError, Line: "error"})

	// Only errors
	lines := buf.Last(10, slog.LevelError)
	if len(lines) != 1 || lines[0] != "error" {
		t.Errorf("error filter: got %v", lines)
	}

	// Warn and above
	lines = buf.Last(10, slog.LevelWarn)
	if len(lines) != 2 || lines[0] != "warn" || lines[1] != "error" {
		t.Errorf("warn filter: got %v", lines)
	}

	// Last 1 at warn level should be the error (most recent matching)
	lines = buf.Last(1, slog.LevelWarn)
	if len(lines) != 1 || lines[0] != "error" {
		t.Errorf("warn filter n=1: got %v", lines)
	}

	// Debug shows all
	lines = buf.Last(10, slog.LevelDebug)
	if len(lines) != 4 {
		t.Errorf("debug filter: got %d lines, want 4", len(lines))
	}
}

func TestLogBufferSubscribe(t *testing.T) {
	buf := NewLogBuffer(100)

	ch := buf.Subscribe()
	defer buf.Unsubscribe(ch)

	buf.Append(entry("live line"))

	select {
	case e := <-ch:
		if e.Line != "live line" {
			t.Errorf("got %q, want %q", e.Line, "live line")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for subscribed line")
	}
}

func TestLogBufferUnsubscribe(t *testing.T) {
	buf := NewLogBuffer(100)

	ch := buf.Subscribe()
	buf.Unsubscribe(ch)

	buf.Append(entry("should not block"))
}

func TestLogHandlerWritesToBuffer(t *testing.T) {
	buf := NewLogBuffer(100)
	handler := NewLogHandler(buf, nil)
	logger := slog.New(handler)

	logger.Info("test message", "key", "value")

	lines := buf.Last(1, slog.LevelDebug)
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	if !containsAll(lines[0], "INF", "test message", "key=value") {
		t.Errorf("line = %q, expected it to contain INF, test message, key=value", lines[0])
	}
}

func TestLogHandlerStreamsToSubscriber(t *testing.T) {
	buf := NewLogBuffer(100)
	handler := NewLogHandler(buf, nil)
	logger := slog.New(handler)

	ch := buf.Subscribe()
	defer buf.Unsubscribe(ch)

	logger.Warn("live warning")

	select {
	case e := <-ch:
		if !containsAll(e.Line, "WRN", "live warning") {
			t.Errorf("line = %q, expected WRN and live warning", e.Line)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for log line")
	}
}

func TestLogHandlerCapturesAllLevels(t *testing.T) {
	buf := NewLogBuffer(100)
	handler := NewLogHandler(buf, nil)
	logger := slog.New(handler)

	logger.Debug("debug msg")
	logger.Info("info msg")
	logger.Warn("warn msg")

	lines := buf.Last(10, slog.LevelDebug)
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3 (handler should capture all levels)", len(lines))
	}
}

func TestLogHandlerEnabled(t *testing.T) {
	buf := NewLogBuffer(100)
	handler := NewLogHandler(buf, &slog.HandlerOptions{Level: slog.LevelError})

	if handler.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("expected INFO to be disabled at ERROR level")
	}
	if !handler.Enabled(context.Background(), slog.LevelError) {
		t.Error("expected ERROR to be enabled at ERROR level")
	}
}

func containsAll(s string, substrings ...string) bool {
	for _, sub := range substrings {
		if !contains(s, sub) {
			return false
		}
	}
	return true
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && searchString(s, sub)
}

func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
