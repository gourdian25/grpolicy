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

// TestChildren_MemberNodeAndChainNode exercises children()'s new cases
// directly (Stage 1 of the member-access feature — see
// docs/plan/member-access-support-plan.md and docs/architecture.md §12).
// MemberNode/ChainNode aren't evaluated yet at this stage, so this is the
// only way to prove children() sees them before evalMember/evalChain exist.
func TestChildren_MemberNodeAndChainNode(t *testing.T) {
	base := &ast.IdentifierNode{Value: "a"}
	prop := &ast.StringNode{Value: "b"}
	member := &ast.MemberNode{Node: base, Property: prop}

	got := children(member)
	if len(got) != 2 || got[0] != ast.Node(base) || got[1] != ast.Node(prop) {
		t.Fatalf("children(MemberNode) = %v, want [base, prop]", got)
	}

	chain := &ast.ChainNode{Node: member}
	gotChain := children(chain)
	if len(gotChain) != 1 || gotChain[0] != ast.Node(member) {
		t.Fatalf("children(ChainNode) = %v, want [member]", gotChain)
	}
}

// TestNodeSpan_MemberNodeDotForm proves the generic children-span-union path
// (now that MemberNode is in children()) produces the correct span for dot
// access with no bracket-matching needed, for both a single link and a
// multi-link chain.
func TestNodeSpan_MemberNodeDotForm(t *testing.T) {
	source := "a.b"
	base := &ast.IdentifierNode{Value: "a"}
	base.SetLocation(file.Location{From: 0, To: 1})
	prop := &ast.StringNode{Value: "b"}
	prop.SetLocation(file.Location{From: 2, To: 3})
	member := &ast.MemberNode{Node: base, Property: prop}
	member.SetLocation(file.Location{From: 2, To: 3}) // MemberNode.Location() covers only the property, per expr-lang/expr.

	if got := exprText(member, source); got != "a.b" {
		t.Fatalf("exprText(a.b) = %q, want %q", got, "a.b")
	}

	source2 := "a.b.c"
	base2 := &ast.IdentifierNode{Value: "a"}
	base2.SetLocation(file.Location{From: 0, To: 1})
	prop2 := &ast.StringNode{Value: "b"}
	prop2.SetLocation(file.Location{From: 2, To: 3})
	member2 := &ast.MemberNode{Node: base2, Property: prop2}
	member2.SetLocation(file.Location{From: 2, To: 3})
	prop3 := &ast.StringNode{Value: "c"}
	prop3.SetLocation(file.Location{From: 4, To: 5})
	member3 := &ast.MemberNode{Node: member2, Property: prop3}
	member3.SetLocation(file.Location{From: 4, To: 5})

	if got := exprText(member3, source2); got != "a.b.c" {
		t.Fatalf("exprText(a.b.c) = %q, want %q", got, "a.b.c")
	}
}

// TestNodeSpan_MemberNodeBracketForm proves the bracket-matching fix: without
// it, the naive children-span-union would miss the trailing "]" (the same
// regression class §11 already fixed for CallNode/ArrayNode).
func TestNodeSpan_MemberNodeBracketForm(t *testing.T) {
	source := `a["b"]`
	base := &ast.IdentifierNode{Value: "a"}
	base.SetLocation(file.Location{From: 0, To: 1})
	prop := &ast.StringNode{Value: "b"}
	prop.SetLocation(file.Location{From: 2, To: 5}) // covers `"b"`, not the brackets.
	member := &ast.MemberNode{Node: base, Property: prop}
	member.SetLocation(file.Location{From: 1, To: 2}) // MemberNode.Location() covers only "[", per expr-lang/expr.

	if got := exprText(member, source); got != `a["b"]` {
		t.Fatalf("exprText(a[\"b\"]) = %q, want %q", got, `a["b"]`)
	}
}

// TestNodeSpan_ChainNodeTransparent proves ChainNode's span is exactly its
// wrapped node's own (already-correct) span, since ChainNode.Location()
// itself covers only a fragment (see docs/architecture.md §12).
func TestNodeSpan_ChainNodeTransparent(t *testing.T) {
	source := "a?.b"
	base := &ast.IdentifierNode{Value: "a"}
	base.SetLocation(file.Location{From: 0, To: 1})
	prop := &ast.StringNode{Value: "b"}
	prop.SetLocation(file.Location{From: 3, To: 4})
	member := &ast.MemberNode{Node: base, Property: prop, Optional: true}
	member.SetLocation(file.Location{From: 3, To: 4})
	chain := &ast.ChainNode{Node: member}
	chain.SetLocation(file.Location{From: 3, To: 4}) // ChainNode.Location() covers only a fragment too.

	if got := exprText(chain, source); got != "a?.b" {
		t.Fatalf("exprText(a?.b) = %q, want %q", got, "a?.b")
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
