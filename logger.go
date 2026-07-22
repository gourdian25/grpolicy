// File: logger.go

package grpolicy

// Logger is the minimal logging interface grpolicy accepts for optional
// diagnostic logging. Its four methods match *slog.Logger's own signatures
// exactly, so *slog.Logger satisfies it structurally — grpolicy itself does
// not import grlog or log/slog, so plugging in a logger is entirely opt-in
// and adds no dependency for consumers who don't want one. Any logger
// exposing the same four methods works.
//
// A nil Logger passed to NewEngine is replaced with NopLogger() — logging
// is always optional, never required for an Engine to function.
//
// Example, using grlog via its log/slog adapter (grlog itself has no
// direct dependents; slog.New(grlog.NewSlogHandler(...)) is the bridge):
//
//	import (
//		"log/slog"
//
//		"github.com/gourdian25/grlog"
//	)
//
//	logger := slog.New(grlog.NewSlogHandler(grlog.NewDefaultLogger()))
//	engine, err := grpolicy.NewEngine(grpolicy.WithLogger(logger)) // *slog.Logger satisfies grpolicy.Logger directly
type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

type noopLogger struct{}

func (noopLogger) Debug(string, ...any) {}
func (noopLogger) Info(string, ...any)  {}
func (noopLogger) Warn(string, ...any)  {}
func (noopLogger) Error(string, ...any) {}

// NopLogger returns a Logger that discards every message. It is the default
// used when no Logger is configured via WithLogger.
//
// Returns:
//   - Logger: a non-nil, no-op implementation safe to call from any goroutine
func NopLogger() Logger { return noopLogger{} }

// OrNop returns l if it is non-nil, otherwise NopLogger(). NewEngine calls
// this once at construction time so every subsequent log call site can
// assume a non-nil Logger.
//
// Parameters:
//   - l: Logger — may be nil
//
// Returns:
//   - Logger: l unchanged if non-nil, otherwise NopLogger()
func OrNop(l Logger) Logger {
	if l == nil {
		return NopLogger()
	}
	return l
}
