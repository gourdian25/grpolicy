// File: internal_coverage_test.go

package grpolicy

import (
	"testing"
	"time"

	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/file"
)

// These tests construct AST nodes and call unexported helpers directly,
// bypassing Compile/Evaluate, to reach a handful of purely defensive
// branches that no input reachable through the public API can trigger:
// either an earlier validation already forecloses it (e.g. Compile's own
// validateFunctionCalls always rejects what evalCall/evalBuiltin's own
// duplicate checks guard against), or the branch reacts to a shape no real
// parser output ever produces (a nil AST node, unbalanced brackets in
// already-validated source).

func TestMeasureComplexity_NilNode(t *testing.T) {
	nodeCount, depth := measureComplexity(nil)
	if nodeCount != 0 || depth != 0 {
		t.Fatalf("measureComplexity(nil) = (%d, %d), want (0, 0)", nodeCount, depth)
	}
}

func TestValidateFunctionCalls_NilNode(t *testing.T) {
	if err := validateFunctionCalls(nil, newFunctionRegistry()); err != nil {
		t.Fatalf("validateFunctionCalls(nil, ...) = %v, want nil", err)
	}
}

func TestNodeSpan_SkipsNilChild(t *testing.T) {
	left := &ast.IdentifierNode{Value: "x"}
	left.SetLocation(file.Location{From: 0, To: 1})
	node := &ast.BinaryNode{Operator: "&&", Left: left, Right: nil}

	// Must not panic on the nil Right child; the exact span returned
	// doesn't matter here, only that traversal tolerates it.
	nodeSpan(node, "x")
}

func TestMatchClosingFrom_DefensiveFallbacks(t *testing.T) {
	t.Run("NotAnOpeningChar", func(t *testing.T) {
		if got := matchClosingFrom("abc", 0, '(', ')'); got != 0 {
			t.Fatalf("matchClosingFrom = %d, want 0 (searchFrom unchanged, source[0] is not '(')", got)
		}
	})
	t.Run("UnbalancedNeverCloses", func(t *testing.T) {
		if got := matchClosingFrom("(abc", 0, '(', ')'); got != 0 {
			t.Fatalf("matchClosingFrom = %d, want 0 (searchFrom unchanged, no balanced close found)", got)
		}
	})
}

func TestExprText_OutOfBoundsSpanReturnsEmpty(t *testing.T) {
	n := &ast.IdentifierNode{Value: "x"}
	n.SetLocation(file.Location{From: 100, To: 200})

	if got := exprText(n, "short"); got != "" {
		t.Fatalf("exprText (out-of-bounds span) = %q, want %q", got, "")
	}
}

func TestPolicyCache_PutOverwritesExistingKey(t *testing.T) {
	c := newPolicyCache(10)
	p1 := &compiledPolicy{source: "a"}
	p2 := &compiledPolicy{source: "a"}

	c.put("k", p1)
	c.put("k", p2)

	got, ok := c.get("k")
	if !ok || got != p2 {
		t.Fatalf("get(k) = (%v, %v), want (p2, true) — second put should overwrite, not duplicate", got, ok)
	}
}

// TestEvalCall_NonIdentifierCalleeDefensiveCheck exercises evalCall's own
// copy of the non-identifier-callee rejection. Unreachable via Compile+
// Evaluate (Compile's validateFunctionCalls always rejects the same shape
// first — see TestCompile_MemberCallTargetUnsupported in coverage_test.go)
// — covered here by driving a walker directly against a hand-built
// CallNode, bypassing Compile entirely.
func TestEvalCall_NonIdentifierCalleeDefensiveCheck(t *testing.T) {
	w := &walker{attrs: map[string]any{}, fns: newFunctionRegistry(), source: "bogus()"}
	node := &ast.CallNode{Callee: &ast.StringNode{Value: "not-an-identifier"}}

	_, _, err := w.evalCall(node)
	if err == nil {
		t.Fatal("evalCall with a non-identifier callee returned nil error, want a rejection")
	}
}

// TestEvalBuiltin_UnsupportedNameDefensiveCheck exercises evalBuiltin's own
// default case. Unreachable via Compile+Evaluate (Compile's
// validateFunctionCalls restricts *ast.BuiltinNode names to
// supportedBuiltinFunctions before Evaluate ever runs) — covered here by
// calling evalBuiltin directly with a name that could never survive
// Compile.
func TestEvalBuiltin_UnsupportedNameDefensiveCheck(t *testing.T) {
	w := &walker{attrs: map[string]any{}, fns: newFunctionRegistry(), source: "bogus()"}
	node := &ast.BuiltinNode{Name: "bogus"}

	_, _, err := w.evalBuiltin(node)
	if err == nil {
		t.Fatal("evalBuiltin with an unsupported name returned nil error, want a rejection")
	}
}

// TestNumericStringTimeCompare_UnsupportedOperatorDefensiveCheck exercises
// numericCompare/stringCompare/timeCompare's own default cases directly.
// All three are unreachable via the public API: compareValues only ever
// forwards an operator that evalComparison itself already restricted to
// "==", "!=", "<", "<=", ">", ">=" (see evalBinary's dispatch switch), so
// none of these functions' own "default" branches can be reached through
// any expression grpolicy will actually evaluate.
func TestNumericStringTimeCompare_UnsupportedOperatorDefensiveCheck(t *testing.T) {
	if _, err := numericCompare("%", 1, 2); err == nil {
		t.Fatal("numericCompare with an unsupported operator returned nil error")
	}
	if _, err := stringCompare("%", "a", "b"); err == nil {
		t.Fatal("stringCompare with an unsupported operator returned nil error")
	}
	if _, err := timeCompare("%", time.Now(), time.Now()); err == nil {
		t.Fatal("timeCompare with an unsupported operator returned nil error")
	}
}

// TestMustRegisterBuiltin_PanicsOnRegistrationFailure exercises the
// panic path documented on mustRegisterBuiltin's own doc comment ("this
// cannot fail" for grpolicy's fixed, literal built-in calls) — unreachable
// via newFunctionRegistry's own real calls, which never use a colliding
// name, but a legitimate direct test of the "this must never happen"
// contract itself, the same idiom as regexp.MustCompile.
func TestMustRegisterBuiltin_PanicsOnRegistrationFailure(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("mustRegisterBuiltin did not panic for a name colliding with a reserved builtin identifier")
		}
	}()
	mustRegisterBuiltin(newFunctionRegistry(), "now", func() bool { return true })
}

// TestFunctionRegistry_CallUnregisteredNameDefensiveCheck exercises
// functionRegistry.call's own "not registered" branch directly.
// Unreachable via Compile+Evaluate: Compile's validateFunctionCalls always
// validates every CallNode's callee against the registry before Evaluate
// ever runs, and functionRegistry has no "unregister" operation, so a name
// that passed Compile-time validation is guaranteed to still be present
// when call() runs.
func TestFunctionRegistry_CallUnregisteredNameDefensiveCheck(t *testing.T) {
	r := newFunctionRegistry()
	if _, err := r.call("neverRegistered", nil); err == nil {
		t.Fatal("call with an unregistered name returned nil error")
	}
}
