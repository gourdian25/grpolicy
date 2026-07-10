// File: errors.go

package grpolicy

import "errors"

// Sentinel errors for use with errors.Is. There is deliberately no
// IsX(err error) bool helper for any of these: callers use
// errors.Is(err, grpolicy.ErrX) directly, consistent with how
// gourdiantoken/grcache/grevents/graudit's own sentinel errors are consumed.
var (
	// ErrCompileFailed indicates the expression could not be parsed.
	ErrCompileFailed = errors.New("grpolicy: expression failed to compile")

	// ErrTooComplex indicates the expression exceeds the configured AST
	// node-count or nesting-depth limit. Returned by Compile, never by
	// Evaluate — a pathologically large or deep expression is rejected
	// before it ever reaches evaluation.
	ErrTooComplex = errors.New("grpolicy: expression exceeds complexity limit")

	// ErrUnknownFunction indicates the expression calls a function name
	// that was not registered via RegisterFunction before Compile was
	// called. Returned by Compile, not Evaluate — every function reference
	// is validated at compile time.
	ErrUnknownFunction = errors.New("grpolicy: reference to unregistered function")

	// ErrEvaluationTimeout indicates Evaluate's context was cancelled or
	// exceeded its deadline before evaluation completed.
	ErrEvaluationTimeout = errors.New("grpolicy: evaluation exceeded context deadline")

	// ErrTypeMismatch indicates an attribute's runtime type is incompatible
	// with the operator applied to it (e.g. comparing a string attribute
	// against a numeric literal with <). There is no silent coercion.
	ErrTypeMismatch = errors.New("grpolicy: attribute type incompatible with operator")

	// ErrAttributeMissing indicates the expression references an attribute
	// key that is not present in the map passed to Evaluate. There is no
	// silent substitution of a zero value — a missing attribute always
	// fails loud.
	ErrAttributeMissing = errors.New("grpolicy: referenced attribute not present in map")

	// ErrClosed indicates a method was called on an Engine after Close.
	ErrClosed = errors.New("grpolicy: engine is closed")
)
