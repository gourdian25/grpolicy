// File: logger.go

package grpolicy

// Logger is the minimal logging interface grpolicy accepts for optional
// diagnostic logging. It is satisfied structurally by *grlog.Logger's
// printf-style methods (Infof/Warnf/Errorf) — grpolicy itself does not
// import grlog, so plugging in a logger is entirely opt-in and adds no
// dependency for consumers who don't want one. Any logger exposing the same
// three methods works; grlog is simply the ecosystem's own recommended
// choice.
//
// A nil Logger passed to NewEngine is replaced with NopLogger() — logging
// is always optional, never required for an Engine to function.
//
// Example:
//
//	import "github.com/gourdian25/grlog"
//
//	logger := grlog.NewDefaultLogger()
//	engine, err := grpolicy.NewEngine(grpolicy.WithLogger(logger)) // *grlog.Logger satisfies grpolicy.Logger directly
type Logger interface {
	Infof(format string, args ...interface{})
	Warnf(format string, args ...interface{})
	Errorf(format string, args ...interface{})
}

type noopLogger struct{}

func (noopLogger) Infof(string, ...interface{})  {}
func (noopLogger) Warnf(string, ...interface{})  {}
func (noopLogger) Errorf(string, ...interface{}) {}

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
