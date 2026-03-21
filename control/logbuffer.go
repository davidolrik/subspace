package control

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"go.olrik.dev/subspace/internal/style"
)

// LogEntry holds a structured log record for deferred formatting.
// Color is applied at output time rather than at capture time, so that
// the decision can be made by the client (which knows whether its
// output is a terminal).
type LogEntry struct {
	Level   slog.Level
	Time    time.Time
	Message string
	Attrs   string // pre-joined "key=val key=val" plain text
}

// LogBuffer is a thread-safe ring buffer that stores log entries and supports
// live subscriptions for streaming.
type LogBuffer struct {
	mu          sync.RWMutex
	entries     []LogEntry
	capacity    int
	writePos    int
	count       int
	subscribers map[chan LogEntry]struct{}
	subs        []chan LogEntry // reusable scratch slice for Append
}

// NewLogBuffer creates a LogBuffer with the given capacity.
func NewLogBuffer(capacity int) *LogBuffer {
	return &LogBuffer{
		entries:     make([]LogEntry, capacity),
		capacity:    capacity,
		subscribers: make(map[chan LogEntry]struct{}),
	}
}

// Append adds a line to the buffer and notifies all subscribers.
func (b *LogBuffer) Append(entry LogEntry) {
	b.mu.Lock()
	b.entries[b.writePos] = entry
	b.writePos = (b.writePos + 1) % b.capacity
	if b.count < b.capacity {
		b.count++
	}
	// Reuse a slice to avoid allocating on every append
	b.subs = b.subs[:0]
	for ch := range b.subscribers {
		b.subs = append(b.subs, ch)
	}
	b.mu.Unlock()

	for _, ch := range b.subs {
		select {
		case ch <- entry:
		default:
		}
	}
}

// Last returns the last n entries from the buffer matching the minimum level,
// in chronological order. If fewer than n entries match, returns all that match.
func (b *LogBuffer) Last(n int, minLevel slog.Level) []LogEntry {
	b.mu.RLock()
	defer b.mu.RUnlock()

	// Scan backwards to find the last n matching entries
	var matched []LogEntry
	for i := 0; i < b.count && len(matched) < n; i++ {
		idx := (b.writePos - 1 - i + b.capacity) % b.capacity
		e := b.entries[idx]
		if e.Level >= minLevel {
			matched = append(matched, e)
		}
	}

	// Reverse to chronological order
	for i, j := 0, len(matched)-1; i < j; i, j = i+1, j-1 {
		matched[i], matched[j] = matched[j], matched[i]
	}
	return matched
}

// Subscribe returns a channel that receives new log entries as they are appended.
func (b *LogBuffer) Subscribe() chan LogEntry {
	ch := make(chan LogEntry, 256)
	b.mu.Lock()
	b.subscribers[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

// Unsubscribe removes a subscription channel.
func (b *LogBuffer) Unsubscribe(ch chan LogEntry) {
	b.mu.Lock()
	delete(b.subscribers, ch)
	b.mu.Unlock()
}

// LogHandler is an slog.Handler that formats log records as colored text and
// writes them to a LogBuffer.
type LogHandler struct {
	buf   *LogBuffer
	level slog.Leveler
	attrs []slog.Attr
	group string
}

// NewLogHandler creates a handler that writes to the given LogBuffer.
// opts may be nil for defaults.
func NewLogHandler(buf *LogBuffer, opts *slog.HandlerOptions) *LogHandler {
	var level slog.Leveler
	if opts != nil && opts.Level != nil {
		level = opts.Level
	} else {
		level = slog.LevelDebug
	}
	return &LogHandler{
		buf:   buf,
		level: level,
	}
}

func (h *LogHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level.Level()
}

func (h *LogHandler) Handle(_ context.Context, r slog.Record) error {
	prefix := ""
	if h.group != "" {
		prefix = h.group + "."
	}

	var attrs []string
	for _, a := range h.attrs {
		attrs = append(attrs, prefix+a.Key+"="+a.Value.String())
	}
	r.Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, prefix+a.Key+"="+a.Value.String())
		return true
	})

	h.buf.Append(LogEntry{
		Level:   r.Level,
		Time:    r.Time,
		Message: r.Message,
		Attrs:   strings.Join(attrs, " "),
	})
	return nil
}

func (h *LogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newAttrs := make([]slog.Attr, len(h.attrs)+len(attrs))
	copy(newAttrs, h.attrs)
	copy(newAttrs[len(h.attrs):], attrs)
	return &LogHandler{
		buf:   h.buf,
		level: h.level,
		attrs: newAttrs,
		group: h.group,
	}
}

func (h *LogHandler) WithGroup(name string) slog.Handler {
	newGroup := name
	if h.group != "" {
		newGroup = h.group + "." + name
	}
	return &LogHandler{
		buf:   h.buf,
		level: h.level,
		attrs: h.attrs,
		group: newGroup,
	}
}

// FormatEntry renders a LogEntry as a single line. When color is true,
// ANSI escape codes are included for terminal display.
func FormatEntry(e LogEntry, color bool) string {
	ts := e.Time.Format(time.DateTime)
	lvl := levelTag(e.Level)
	msg := e.Message
	if color {
		ts = style.ForceColorize(style.Ghost, ts)
		lvl = style.ForceColorLevel(e.Level)
		msg = style.ForceBoldC(style.Steel, msg)
	}

	line := fmt.Sprintf("%s %s %s", ts, lvl, msg)
	if e.Attrs != "" {
		if color {
			line += " " + colorAttrs(e.Attrs)
		} else {
			line += " " + e.Attrs
		}
	}
	return line
}

// levelTag returns a plain text level label.
func levelTag(level slog.Level) string {
	switch {
	case level >= slog.LevelError:
		return "ERR"
	case level >= slog.LevelWarn:
		return "WRN"
	case level >= slog.LevelInfo:
		return "INF"
	default:
		return "DBG"
	}
}

// colorAttrs applies color to a pre-joined "key=val key=val" string.
func colorAttrs(attrs string) string {
	var parts []string
	for _, pair := range strings.Split(attrs, " ") {
		if eq := strings.IndexByte(pair, '='); eq >= 0 {
			key := pair[:eq]
			val := pair[eq+1:]
			colored := style.ForceColorize(style.Smoke, key) +
				style.ForceColorize(style.Ghost, "=") +
				style.ForceColorize(style.Green, val)
			parts = append(parts, colored)
		} else {
			parts = append(parts, pair)
		}
	}
	return strings.Join(parts, " ")
}
