package control

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"go.olrik.dev/subspace/internal/style"
)

// LogEntry holds a log line together with its level for filtering.
type LogEntry struct {
	Level slog.Level
	Line  string
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

// Last returns the last n lines from the buffer matching the minimum level,
// in chronological order. If fewer than n lines match, returns all that match.
func (b *LogBuffer) Last(n int, minLevel slog.Level) []string {
	b.mu.RLock()
	defer b.mu.RUnlock()

	// Scan backwards to find the last n matching entries
	var matched []string
	for i := 0; i < b.count && len(matched) < n; i++ {
		idx := (b.writePos - 1 - i + b.capacity) % b.capacity
		e := b.entries[idx]
		if e.Level >= minLevel {
			matched = append(matched, e.Line)
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
	ts := style.Colorize(style.Ghost, r.Time.Format(time.DateTime))
	lvl := style.ColorLevel(r.Level)
	msg := style.BoldC(style.Steel, r.Message)

	line := fmt.Sprintf("%s %s %s", ts, lvl, msg)

	prefix := ""
	if h.group != "" {
		prefix = h.group + "."
	}

	for _, a := range h.attrs {
		line += " " + style.FormatAttr(prefix, a)
	}

	r.Attrs(func(a slog.Attr) bool {
		line += " " + style.FormatAttr(prefix, a)
		return true
	})

	h.buf.Append(LogEntry{Level: r.Level, Line: line})
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
