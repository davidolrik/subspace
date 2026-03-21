package style

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"
)

// LogHandler is a colored slog.Handler that writes to an io.Writer.
type LogHandler struct {
	w     io.Writer
	level slog.Leveler
	attrs []slog.Attr
	group string
}

// NewLogHandler creates a colored log handler writing to w.
func NewLogHandler(w io.Writer, opts *slog.HandlerOptions) *LogHandler {
	var level slog.Leveler
	if opts != nil && opts.Level != nil {
		level = opts.Level
	} else {
		level = slog.LevelInfo
	}
	return &LogHandler{w: w, level: level}
}

func (h *LogHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level.Level()
}

func (h *LogHandler) Handle(_ context.Context, r slog.Record) error {
	ts := Colorize(Ghost, r.Time.Format(time.DateTime))
	lvl := ColorLevel(r.Level)
	msg := BoldC(Steel, r.Message)

	line := fmt.Sprintf("%s %s %s", ts, lvl, msg)

	prefix := ""
	if h.group != "" {
		prefix = h.group + "."
	}

	for _, a := range h.attrs {
		line += " " + FormatAttr(prefix, a)
	}

	r.Attrs(func(a slog.Attr) bool {
		line += " " + FormatAttr(prefix, a)
		return true
	})

	_, err := fmt.Fprintln(h.w, line)
	return err
}

func (h *LogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newAttrs := make([]slog.Attr, len(h.attrs)+len(attrs))
	copy(newAttrs, h.attrs)
	copy(newAttrs[len(h.attrs):], attrs)
	return &LogHandler{w: h.w, level: h.level, attrs: newAttrs, group: h.group}
}

func (h *LogHandler) WithGroup(name string) slog.Handler {
	newGroup := name
	if h.group != "" {
		newGroup = h.group + "." + name
	}
	return &LogHandler{w: h.w, level: h.level, attrs: h.attrs, group: newGroup}
}

// ColorLevel returns a colored level badge, respecting the terminal detection flag.
func ColorLevel(level slog.Level) string {
	return colorLevel(level, Badge)
}

// ForceColorLevel returns a colored level badge regardless of the terminal detection flag.
func ForceColorLevel(level slog.Level) string {
	return colorLevel(level, ForceBadge)
}

func colorLevel(level slog.Level, badge func(string, string, string) string) string {
	switch {
	case level >= slog.LevelError:
		return badge(Red, bgErr, "ERR")
	case level >= slog.LevelWarn:
		return badge(Yellow, bgWarn, "WRN")
	case level >= slog.LevelInfo:
		return badge(Cyan, bgInfo, "INF")
	default:
		return badge(Ghost, bgDbg, "DBG")
	}
}

// FormatAttr formats a key=value pair with cyber styling.
func FormatAttr(prefix string, a slog.Attr) string {
	key := Colorize(Smoke, prefix+a.Key)
	val := a.Value.String()
	if a.Key == "via" {
		val = BoldC(UpstreamColor(val), val)
	} else {
		val = Colorize(Green, val)
	}
	return key + Colorize(Ghost, "=") + val
}
