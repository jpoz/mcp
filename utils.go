package mcp

import (
	"context"
	"log/slog"
)

// noopHandler is a slog handler that does nothing.
type noopHandler struct{}

// Enabled always returns false to skip logging.
func (h noopHandler) Enabled(context.Context, slog.Level) bool {
	return false
}

// Handle does nothing.
func (h noopHandler) Handle(context.Context, slog.Record) error {
	return nil
}

// WithAttrs returns itself as attributes have no effect.
func (h noopHandler) WithAttrs([]slog.Attr) slog.Handler {
	return h
}

// WithGroup returns itself as groups have no effect.
func (h noopHandler) WithGroup(string) slog.Handler {
	return h
}

// NewLoggingManager creates a new logging manager
func NewNoopLogger() *slog.Logger {
	return slog.New(&noopHandler{})
}
