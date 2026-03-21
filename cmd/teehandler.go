package cmd

import (
	"context"
	"log/slog"
)

// teeHandler sends log records to two slog handlers.
type teeHandler struct {
	a, b slog.Handler
}

func newTeeHandler(a, b slog.Handler) *teeHandler {
	return &teeHandler{a: a, b: b}
}

func (h *teeHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.a.Enabled(ctx, level) || h.b.Enabled(ctx, level)
}

func (h *teeHandler) Handle(ctx context.Context, r slog.Record) error {
	if h.a.Enabled(ctx, r.Level) {
		h.a.Handle(ctx, r.Clone())
	}
	if h.b.Enabled(ctx, r.Level) {
		h.b.Handle(ctx, r.Clone())
	}
	return nil
}

func (h *teeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &teeHandler{
		a: h.a.WithAttrs(attrs),
		b: h.b.WithAttrs(attrs),
	}
}

func (h *teeHandler) WithGroup(name string) slog.Handler {
	return &teeHandler{
		a: h.a.WithGroup(name),
		b: h.b.WithGroup(name),
	}
}
