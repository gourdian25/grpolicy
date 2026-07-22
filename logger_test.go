// File: logger_test.go

package grpolicy_test

import (
	"log/slog"
	"testing"

	"github.com/gourdian25/grlog"
	"github.com/gourdian25/grpolicy"
)

// *slog.Logger satisfies grpolicy.Logger structurally — no adapter needed.
var _ grpolicy.Logger = (*slog.Logger)(nil)

// TestGrlogSatisfiesLoggerInterface proves the recommended integration path
// (slog.New(grlog.NewSlogHandler(...))) actually compiles and runs without
// panicking, end to end.
func TestGrlogSatisfiesLoggerInterface(t *testing.T) {
	logger := grlog.NewDefaultLogger()
	defer func() { _ = logger.Close() }()

	slogger := slog.New(grlog.NewSlogHandler(logger))
	var l grpolicy.Logger = slogger

	l.Debug("grpolicy test", "level", "debug")
	l.Info("grpolicy test", "level", "info")
	l.Warn("grpolicy test", "level", "warn")
	l.Error("grpolicy test", "level", "error")
}

// TestNopLogger proves the no-op implementation never panics.
func TestNopLogger(t *testing.T) {
	l := grpolicy.NopLogger()
	l.Debug("noop")
	l.Info("noop")
	l.Warn("noop")
	l.Error("noop")
}
