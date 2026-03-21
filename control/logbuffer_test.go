package control

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func entry(msg string) LogEntry {
	return LogEntry{Level: slog.LevelInfo, Time: time.Now(), Message: msg}
}

func TestLogBufferStoresEntries(t *testing.T) {
	buf := NewLogBuffer(100)

	buf.Append(entry("line 1"))
	buf.Append(entry("line 2"))
	buf.Append(entry("line 3"))

	entries := buf.Last(10, slog.LevelDebug)
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}
	if entries[0].Message != "line 1" || entries[1].Message != "line 2" || entries[2].Message != "line 3" {
		t.Errorf("messages = %v", []string{entries[0].Message, entries[1].Message, entries[2].Message})
	}
}

func TestLogBufferWrapsAround(t *testing.T) {
	buf := NewLogBuffer(3)

	buf.Append(entry("a"))
	buf.Append(entry("b"))
	buf.Append(entry("c"))
	buf.Append(entry("d"))

	entries := buf.Last(10, slog.LevelDebug)
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}
	if entries[0].Message != "b" || entries[1].Message != "c" || entries[2].Message != "d" {
		t.Errorf("messages = [%s %s %s], want [b c d]",
			entries[0].Message, entries[1].Message, entries[2].Message)
	}
}

func TestLogBufferLastN(t *testing.T) {
	buf := NewLogBuffer(100)
	for i := 0; i < 20; i++ {
		buf.Append(entry(fmt.Sprintf("line %d", i)))
	}

	entries := buf.Last(5, slog.LevelDebug)
	if len(entries) != 5 {
		t.Fatalf("got %d entries, want 5", len(entries))
	}
	if entries[0].Message != "line 15" || entries[4].Message != "line 19" {
		t.Errorf("first=%q last=%q", entries[0].Message, entries[4].Message)
	}
}

func TestLogBufferLastMoreThanAvailable(t *testing.T) {
	buf := NewLogBuffer(100)
	buf.Append(entry("only"))

	entries := buf.Last(10, slog.LevelDebug)
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
}

func TestLogBufferLastFiltersByLevel(t *testing.T) {
	buf := NewLogBuffer(100)

	buf.Append(LogEntry{Level: slog.LevelDebug, Time: time.Now(), Message: "debug"})
	buf.Append(LogEntry{Level: slog.LevelInfo, Time: time.Now(), Message: "info"})
	buf.Append(LogEntry{Level: slog.LevelWarn, Time: time.Now(), Message: "warn"})
	buf.Append(LogEntry{Level: slog.LevelError, Time: time.Now(), Message: "error"})

	// Only errors
	entries := buf.Last(10, slog.LevelError)
	if len(entries) != 1 || entries[0].Message != "error" {
		t.Errorf("error filter: got %v", entries)
	}

	// Warn and above
	entries = buf.Last(10, slog.LevelWarn)
	if len(entries) != 2 || entries[0].Message != "warn" || entries[1].Message != "error" {
		t.Errorf("warn filter: got %v", entries)
	}

	// Last 1 at warn level should be the error (most recent matching)
	entries = buf.Last(1, slog.LevelWarn)
	if len(entries) != 1 || entries[0].Message != "error" {
		t.Errorf("warn filter n=1: got %v", entries)
	}

	// Debug shows all
	entries = buf.Last(10, slog.LevelDebug)
	if len(entries) != 4 {
		t.Errorf("debug filter: got %d entries, want 4", len(entries))
	}
}

func TestLogBufferSubscribe(t *testing.T) {
	buf := NewLogBuffer(100)

	ch := buf.Subscribe()
	defer buf.Unsubscribe(ch)

	buf.Append(entry("live line"))

	select {
	case e := <-ch:
		if e.Message != "live line" {
			t.Errorf("got %q, want %q", e.Message, "live line")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for subscribed entry")
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

	entries := buf.Last(1, slog.LevelDebug)
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	e := entries[0]
	if e.Message != "test message" {
		t.Errorf("message = %q, want %q", e.Message, "test message")
	}
	if e.Level != slog.LevelInfo {
		t.Errorf("level = %v, want INFO", e.Level)
	}
	if !strings.Contains(e.Attrs, "key=value") {
		t.Errorf("attrs = %q, expected it to contain key=value", e.Attrs)
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
		if e.Message != "live warning" {
			t.Errorf("message = %q, want %q", e.Message, "live warning")
		}
		if e.Level != slog.LevelWarn {
			t.Errorf("level = %v, want WARN", e.Level)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for log entry")
	}
}

func TestLogHandlerCapturesAllLevels(t *testing.T) {
	buf := NewLogBuffer(100)
	handler := NewLogHandler(buf, nil)
	logger := slog.New(handler)

	logger.Debug("debug msg")
	logger.Info("info msg")
	logger.Warn("warn msg")

	entries := buf.Last(10, slog.LevelDebug)
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3 (handler should capture all levels)", len(entries))
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

func TestFormatEntryPlain(t *testing.T) {
	e := LogEntry{
		Level:   slog.LevelInfo,
		Time:    time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
		Message: "server started",
		Attrs:   "addr=:8118 upstreams=2",
	}

	line := FormatEntry(e, false)
	if !strings.Contains(line, "2025-01-15 10:30:00") {
		t.Errorf("expected timestamp, got %q", line)
	}
	if !strings.Contains(line, "INF") {
		t.Errorf("expected INF, got %q", line)
	}
	if !strings.Contains(line, "server started") {
		t.Errorf("expected message, got %q", line)
	}
	if !strings.Contains(line, "addr=:8118") {
		t.Errorf("expected attrs, got %q", line)
	}
	// Plain mode should have no ANSI escapes
	if strings.Contains(line, "\033[") {
		t.Errorf("plain mode should not contain ANSI escapes, got %q", line)
	}
}

func TestFormatEntryColored(t *testing.T) {
	e := LogEntry{
		Level:   slog.LevelError,
		Time:    time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
		Message: "connection failed",
		Attrs:   "host=example.com",
	}

	line := FormatEntry(e, true)
	if !strings.Contains(line, "connection failed") {
		t.Errorf("expected message, got %q", line)
	}
	if !strings.Contains(line, "ERR") {
		t.Errorf("expected ERR, got %q", line)
	}
	// Colored mode should contain ANSI escapes
	if !strings.Contains(line, "\033[") {
		t.Errorf("color mode should contain ANSI escapes, got %q", line)
	}
}

func TestFormatEntryNoAttrs(t *testing.T) {
	e := LogEntry{
		Level:   slog.LevelInfo,
		Time:    time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
		Message: "ready",
	}

	line := FormatEntry(e, false)
	// Should not have trailing space or empty attrs
	if strings.HasSuffix(line, " ") {
		t.Errorf("line should not have trailing space: %q", line)
	}
}

func TestFormatEntryLevels(t *testing.T) {
	tests := []struct {
		level slog.Level
		tag   string
	}{
		{slog.LevelDebug, "DBG"},
		{slog.LevelInfo, "INF"},
		{slog.LevelWarn, "WRN"},
		{slog.LevelError, "ERR"},
	}

	for _, tt := range tests {
		e := LogEntry{Level: tt.level, Time: time.Now(), Message: "msg"}
		line := FormatEntry(e, false)
		if !strings.Contains(line, tt.tag) {
			t.Errorf("level %v: expected %q in %q", tt.level, tt.tag, line)
		}
	}
}
