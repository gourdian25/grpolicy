// File: fuzz_test.go

package grpolicy_test

import (
	"context"
	"testing"
	"time"

	"github.com/gourdian25/grpolicy"
)

// fuzzMemberAccessInfo gives the fuzz target a struct-valued attribute
// (Stage 3 of the member-access feature) with one exported and one
// unexported field, reachable via the "info.Name"/"info.secret" seeds.
type fuzzMemberAccessInfo struct {
	Name   string
	secret int
}

// FuzzCompileEvaluate feeds arbitrary strings as policy expressions (plus a
// handful of primitive values used to populate a fixed attribute schema)
// through Compile and Evaluate. eval.go is the one piece of code in this
// repo with no upstream test coverage from expr-lang/expr itself — the
// library's own parser is well exercised by its own test suite, but
// grpolicy's tree-walking evaluator on top of the AST it returns is
// entirely new code, so this is the single most important place for
// previously-unseen input to find a panic or a hang.
//
// What this fuzz target checks, on every input:
//  1. Compile never panics, no matter how malformed expr is.
//  2. If Compile succeeds, Evaluate never panics.
//  3. Evaluate always returns within a generous bound even though its own
//     ctx deadline is short — a genuine hang (not just a returned timeout
//     error) is a bug, since grpolicy's evaluator is documented to check
//     ctx.Done() between every sub-expression.
//
// AST recursion depth is already bounded by Compile's complexity-limit walk
// (see measureComplexity in ast_helpers.go and WithMaxComplexity), so a
// pathologically deep expression is rejected before Evaluate's own
// recursive walk would ever run — this fuzz target is what actually proves
// that protection holds against adversarial input, rather than just the
// hand-picked ComplexityLimitRejectedAtCompile conformance case.
func FuzzCompileEvaluate(f *testing.F) {
	seeds := []string{
		`a && b`,
		`a || b`,
		`!a`,
		`not a`,
		`age >= 18`,
		`age == 18 && name == "x"`,
		`name contains "x"`,
		`name startsWith "x"`,
		`name endsWith "x"`,
		`name matches "^a.*"`,
		`name matches "(a+)+$"`,
		`role in ["admin", "user"]`,
		`age in roles`,
		`double(age) == 42`,
		`now() != nil`,
		`duration("1h") != nil`,
		`(a == 1 && (b == 2 || c == 3)) || !enabled`,
		`((((a))))`,
		`a == b == c`,
		`a &&`,
		`&& a`,
		`(a`,
		`a)`,
		``,
		`"unterminated`,
		`a matches "["`,
		`unregisteredFn()`,
		`now`,
		`duration`,
		`nested.x == age`,
		`nested["y"] == name`,
		`nested.missing`,
		`roles[0] == "admin"`,
		`roles[100]`,
		`age[0]`,
		`name[0]`,
		`info.Name == name`,
		`info.secret`,
		`info.Missing`,
		`a?.b`,
		`a?.b?.c`,
		`nested?.x == age`,
		`a.b?.c.d`,
		`a?.[0]`,
		`a?.b[0]`,
	}
	for _, s := range seeds {
		f.Add(s, 5, "hello", true)
	}

	engine, err := grpolicy.NewEngine()
	if err != nil {
		f.Fatalf("NewEngine: %v", err)
	}
	defer engine.Close()
	if err := engine.RegisterFunction("double", func(x int) int { return x * 2 }); err != nil {
		f.Fatalf("RegisterFunction: %v", err)
	}

	f.Fuzz(func(t *testing.T, expr string, i int, s string, b bool) {
		attrs := map[string]any{
			"a":       i,
			"b":       s,
			"c":       b,
			"age":     i,
			"name":    s,
			"enabled": b,
			"role":    s,
			"score":   float64(i),
			"roles":   []string{s, "admin"},
			"nested":  map[string]any{"x": i, "y": s},
			"info":    fuzzMemberAccessInfo{Name: s, secret: i},
		}

		type compileResult struct {
			policy grpolicy.CompiledPolicy
			err    error
		}
		compileDone := make(chan compileResult, 1)
		go func() {
			p, err := engine.Compile(expr)
			compileDone <- compileResult{p, err}
		}()

		var cr compileResult
		select {
		case cr = <-compileDone:
		case <-time.After(2 * time.Second):
			t.Fatalf("Compile(%q) did not return within 2s — parser or complexity walk hang", expr)
			return
		}
		if cr.err != nil {
			return // malformed/rejected input is expected and fine
		}

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		type evalResult struct {
			decision grpolicy.Decision
			err      error
		}
		evalDone := make(chan evalResult, 1)
		go func() {
			d, err := cr.policy.Evaluate(ctx, attrs)
			evalDone <- evalResult{d, err}
		}()

		select {
		case er := <-evalDone:
			// Both a nil and non-nil error are acceptable outcomes; what
			// matters is that Trace is never nil-vs-populated in a way that
			// contradicts the documented partial-trace guarantee, and that
			// we got here at all without panicking.
			if er.err == nil && len(er.decision.Trace) == 0 {
				t.Fatalf("Evaluate(%q) succeeded with an empty Trace, want at least the root node", expr)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("Evaluate(%q) did not return within 2s despite a 50ms ctx deadline — hang past the configured timeout", expr)
		}
	})
}
