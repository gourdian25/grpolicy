// File: coverage_test.go

package grpolicy_test

import (
	"errors"
	"testing"
	"time"

	"github.com/gourdian25/grpolicy"
)

// These tests close gaps left by contract_engine_test.go's behavioral
// suite: real branches reachable through the public API that no existing
// scenario happens to exercise (a raw float/bool literal as a leaf node,
// an arithmetic operator grpolicy doesn't implement, "&&"/"||" erroring on
// either side, a reserved-but-unimplemented builtin name, function-registry
// arity/type/conversion edge cases, and a couple of grpolicy's own
// hardcoded built-ins that were registered but never actually invoked).

func newTestEngine(t *testing.T) grpolicy.Engine {
	t.Helper()
	engine, err := grpolicy.NewEngine()
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	t.Cleanup(func() { _ = engine.Close() })
	return engine
}

func TestEval_FloatAndBoolLiterals(t *testing.T) {
	engine := newTestEngine(t)
	ctx := t.Context()

	policy, err := engine.Compile("x == 3.5")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	decision, err := policy.Evaluate(ctx, map[string]any{"x": 3.5})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("Allowed = false, want true (float literal leaf)")
	}

	policy2, err := engine.Compile("x == true")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	decision2, err := policy2.Evaluate(ctx, map[string]any{"x": true})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !decision2.Allowed {
		t.Fatalf("Allowed = false, want true (bool literal leaf)")
	}
}

func TestEval_UnsupportedBinaryOperatorAtEvaluate(t *testing.T) {
	engine := newTestEngine(t)
	ctx := t.Context()

	// grpolicy never validates binary operators at Compile time (only
	// function calls) — an arithmetic expression parses successfully but
	// fails at Evaluate since evalBinary has no case for "+".
	policy, err := engine.Compile("a + b")
	if err != nil {
		t.Fatalf("Compile(%q): %v", "a + b", err)
	}
	_, err = policy.Evaluate(ctx, map[string]any{"a": 1, "b": 2})
	if err == nil {
		t.Fatalf("Evaluate(a + b) error = nil, want a non-nil unsupported-operator error")
	}
}

func TestEval_AndOrErrorOnEitherSide(t *testing.T) {
	engine := newTestEngine(t)
	ctx := t.Context()

	cases := []struct {
		name  string
		expr  string
		attrs map[string]any
	}{
		{"AndLeftErrors", "missing && b", map[string]any{"b": true}},
		{"OrLeftErrors", "missing || b", map[string]any{"b": true}},
		{"OrRightErrors", "a || missing", map[string]any{"a": false}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			policy, err := engine.Compile(c.expr)
			if err != nil {
				t.Fatalf("Compile(%q): %v", c.expr, err)
			}
			_, err = policy.Evaluate(ctx, c.attrs)
			if !errors.Is(err, grpolicy.ErrAttributeMissing) {
				t.Fatalf("Evaluate(%q) error = %v, want ErrAttributeMissing", c.expr, err)
			}
		})
	}
}

func TestEval_UnaryErrorPropagates(t *testing.T) {
	engine := newTestEngine(t)
	ctx := t.Context()

	policy, err := engine.Compile("!missing")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	_, err = policy.Evaluate(ctx, map[string]any{})
	if !errors.Is(err, grpolicy.ErrAttributeMissing) {
		t.Fatalf("Evaluate(!missing) error = %v, want ErrAttributeMissing", err)
	}
}

// TestEval_ShortCircuitStubsUnaryOperator exercises buildStub's
// operatorOf(*ast.UnaryNode) branch: a "!"-prefixed sub-expression stubbed
// as the unevaluated right side of a short-circuited "&&" must still carry
// its own Operator in the trace.
func TestEval_ShortCircuitStubsUnaryOperator(t *testing.T) {
	engine := newTestEngine(t)
	ctx := t.Context()

	policy, err := engine.Compile("a && !b")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	decision, err := policy.Evaluate(ctx, map[string]any{"a": false, "b": true})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if decision.Allowed {
		t.Fatalf("Allowed = true, want false")
	}
	right := decision.Trace[0].Children[1]
	if right.Evaluated {
		t.Fatalf("right branch Evaluated = true, want false (short-circuited)")
	}
	if right.Operator != "!" {
		t.Fatalf("right branch Operator = %q, want %q", right.Operator, "!")
	}
}

// TestEval_ArrayLiteralStubsRemainingElementsOnError and
// TestEval_CallArgsStubRemainingArgsOnError exercise evalArray's and
// evalArgs' own not-yet-reached-elements stub loop: when an element/arg
// partway through fails, Children must still include a stub for every
// element/arg after it, mirroring the array/call's full static shape —
// the same convention every binary operator already follows for its own
// unevaluated sibling.
func TestEval_ArrayLiteralStubsRemainingElementsOnError(t *testing.T) {
	engine := newTestEngine(t)
	ctx := t.Context()

	policy, err := engine.Compile("a in [1, missing, 3]")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	decision, err := policy.Evaluate(ctx, map[string]any{"a": 1})
	if !errors.Is(err, grpolicy.ErrAttributeMissing) {
		t.Fatalf("Evaluate error = %v, want ErrAttributeMissing", err)
	}
	arrayNode := decision.Trace[0].Children[1]
	if len(arrayNode.Children) != 3 {
		t.Fatalf("array literal Children has %d entries, want 3 (full static shape, including the stub for the un-reached third element)", len(arrayNode.Children))
	}
	if arrayNode.Children[0].Evaluated != true {
		t.Fatalf("array element 0 Evaluated = false, want true (it ran successfully)")
	}
	if arrayNode.Children[1].Evaluated {
		t.Fatalf("array element 1 (the failing one) Evaluated = true, want false")
	}
	if arrayNode.Children[2].Evaluated {
		t.Fatalf("array element 2 (never reached) Evaluated = true, want false (must be a stub)")
	}
}

func TestEval_CallArgsStubRemainingArgsOnError(t *testing.T) {
	engine := newTestEngine(t)
	ctx := t.Context()

	if err := engine.RegisterFunction("sum3", func(a, b, c int) int { return a + b + c }); err != nil {
		t.Fatalf("RegisterFunction: %v", err)
	}

	policy, err := engine.Compile("sum3(1, missing, 3) == 4")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	decision, err := policy.Evaluate(ctx, map[string]any{})
	if !errors.Is(err, grpolicy.ErrAttributeMissing) {
		t.Fatalf("Evaluate error = %v, want ErrAttributeMissing", err)
	}
	callNode := decision.Trace[0].Children[0]
	if len(callNode.Children) != 3 {
		t.Fatalf("call Children has %d entries, want 3 (full static shape, including the stub for the un-reached third argument)", len(callNode.Children))
	}
	if callNode.Children[2].Evaluated {
		t.Fatalf("argument 2 (never reached) Evaluated = true, want false (must be a stub)")
	}
}

// TestCompile_ReservedBuiltinNameNotImplementedByGrpolicy exercises
// validateFunctionCalls' *ast.BuiltinNode branch specifically: "len" and
// "all" are reserved by expr-lang/expr's parser (so a call to either
// always parses as a BuiltinNode, never a CallNode) but grpolicy doesn't
// implement either as one of its own two supported builtins ("now",
// "duration") — distinct from RegisterFunction("len", ...) itself being
// rejected (see FunctionRegistryEdgeCases), this is about what happens
// when such a name is called without ever having been registered at all.
func TestCompile_ReservedBuiltinNameNotImplementedByGrpolicy(t *testing.T) {
	engine := newTestEngine(t)

	_, err := engine.Compile("len(x)")
	if !errors.Is(err, grpolicy.ErrUnknownFunction) {
		t.Fatalf("Compile(len(x)) error = %v, want ErrUnknownFunction", err)
	}
}

// TestCompile_NestedUnknownFunctionErrorPropagates exercises
// validateFunctionCalls' recursive child-propagation branch specifically:
// the earlier ShortCircuitAndTrace/CustomFunctionRegistrationAndInvocation
// tests only ever put the failing call at the tree's root, which returns
// directly without ever reaching the "recurse into children, propagate a
// child's error" loop — this expression fails one level down instead.
func TestCompile_NestedUnknownFunctionErrorPropagates(t *testing.T) {
	engine := newTestEngine(t)

	_, err := engine.Compile("x == 1 && notRegistered()")
	if !errors.Is(err, grpolicy.ErrUnknownFunction) {
		t.Fatalf("Compile error = %v, want ErrUnknownFunction", err)
	}
}

func TestRegisterFunction_NonFunctionValueRejected(t *testing.T) {
	engine := newTestEngine(t)

	err := engine.RegisterFunction("notAFunc", 42)
	if err == nil {
		t.Fatalf("RegisterFunction(42) error = nil, want a rejection (not a function)")
	}
}

func TestFunctionCall_ArityMismatches(t *testing.T) {
	engine := newTestEngine(t)
	ctx := t.Context()

	if err := engine.RegisterFunction("double", func(x int) int { return x * 2 }); err != nil {
		t.Fatalf("RegisterFunction(double): %v", err)
	}
	// Variadic with a required leading parameter: at least one arg needed.
	if err := engine.RegisterFunction("joinInts", func(sep string, nums ...int) string { return sep }); err != nil {
		t.Fatalf("RegisterFunction(joinInts): %v", err)
	}

	t.Run("FixedArityWrongCount", func(t *testing.T) {
		policy, err := engine.Compile("double(1, 2) == 2")
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		_, err = policy.Evaluate(ctx, map[string]any{})
		if !errors.Is(err, grpolicy.ErrTypeMismatch) {
			t.Fatalf("Evaluate error = %v, want ErrTypeMismatch", err)
		}
	})

	t.Run("VariadicBelowRequiredMinimum", func(t *testing.T) {
		policy, err := engine.Compile(`joinInts() == "x"`)
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		_, err = policy.Evaluate(ctx, map[string]any{})
		if !errors.Is(err, grpolicy.ErrTypeMismatch) {
			t.Fatalf("Evaluate error = %v, want ErrTypeMismatch", err)
		}
	})
}

func TestFunctionCall_NilArgumentZeroValues(t *testing.T) {
	engine := newTestEngine(t)
	ctx := t.Context()

	if err := engine.RegisterFunction("isNilArg", func(x any) bool { return x == nil }); err != nil {
		t.Fatalf("RegisterFunction: %v", err)
	}

	policy, err := engine.Compile("isNilArg(nil) == true")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	decision, err := policy.Evaluate(ctx, map[string]any{})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("Allowed = false, want true (nil argument should zero-value into the any parameter)")
	}
}

// TestFunctionCall_TypeMismatchNonConvertibleNumericTarget exercises both
// call()'s type-mismatch return AND isNumericKind's own default (false)
// branch in the same call: an int argument is convertible to a string
// parameter in Go's plain conversion rules (interpreted as a code point),
// but the conversion guard also requires the *parameter* to be numeric,
// which a string never is — so this must still be rejected, and reaching
// that conclusion requires actually calling isNumericKind on the string
// parameter's Kind and getting false back.
func TestFunctionCall_TypeMismatchNonConvertibleNumericTarget(t *testing.T) {
	engine := newTestEngine(t)
	ctx := t.Context()

	if err := engine.RegisterFunction("wantsString", func(s string) bool { return true }); err != nil {
		t.Fatalf("RegisterFunction: %v", err)
	}

	policy, err := engine.Compile("wantsString(age) == true")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	_, err = policy.Evaluate(ctx, map[string]any{"age": 65})
	if !errors.Is(err, grpolicy.ErrTypeMismatch) {
		t.Fatalf("Evaluate error = %v, want ErrTypeMismatch (int -> string is not treated as an allowed numeric conversion)", err)
	}
}

// TestFunctionCall_SingleErrorReturnValue exercises call()'s case-1 branch
// where the sole return value is itself an error (not a (value, error)
// pair) — distinct from "mayFail" in the contract suite, which returns two
// values.
func TestFunctionCall_SingleErrorReturnValue(t *testing.T) {
	engine := newTestEngine(t)
	ctx := t.Context()

	if err := engine.RegisterFunction("validate", func(x int) error {
		if x < 0 {
			return errors.New("negative value")
		}
		return nil
	}); err != nil {
		t.Fatalf("RegisterFunction: %v", err)
	}

	policy, err := engine.Compile("validate(x) == nil")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if _, err := policy.Evaluate(ctx, map[string]any{"x": -1}); err == nil {
		t.Fatalf("Evaluate(x=-1) error = nil, want the returned error to surface")
	}
}

func TestEval_ComparisonStringOpMatchesRightSideErrors(t *testing.T) {
	engine := newTestEngine(t)
	ctx := t.Context()

	cases := []struct {
		name  string
		expr  string
		attrs map[string]any
	}{
		{"ComparisonRightErrors", "a == missing", map[string]any{"a": 1}},
		{"StringOpRightErrors", `a contains missing`, map[string]any{"a": "x"}},
		{"MatchesRightErrors", `a matches missing`, map[string]any{"a": "x"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			policy, err := engine.Compile(c.expr)
			if err != nil {
				t.Fatalf("Compile(%q): %v", c.expr, err)
			}
			_, err = policy.Evaluate(ctx, c.attrs)
			if !errors.Is(err, grpolicy.ErrAttributeMissing) {
				t.Fatalf("Evaluate(%q) error = %v, want ErrAttributeMissing", c.expr, err)
			}
		})
	}
}

func TestEval_BuiltinArgumentErrorPropagates(t *testing.T) {
	engine := newTestEngine(t)
	ctx := t.Context()

	policy, err := engine.Compile("duration(missing) != nil")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	_, err = policy.Evaluate(ctx, map[string]any{})
	if !errors.Is(err, grpolicy.ErrAttributeMissing) {
		t.Fatalf("Evaluate error = %v, want ErrAttributeMissing", err)
	}
}

// TestEval_BuiltinArgumentValidation exercises now()/duration()'s own
// argument-count and argument-type validation in eval.go's evalBuiltin —
// grpolicy's parser never statically type-checks builtin call arity, so
// these are only ever caught at Evaluate time.
func TestEval_BuiltinArgumentValidation(t *testing.T) {
	engine := newTestEngine(t)
	ctx := t.Context()

	cases := []struct {
		name string
		expr string
	}{
		{"NowWithArguments", "now(1) != nil"},
		{"DurationWrongArgCount", "duration() != nil"},
		{"DurationNonStringArgument", "duration(5) != nil"},
		{"DurationInvalidDurationString", `duration("not-a-duration") != nil`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			policy, err := engine.Compile(c.expr)
			if err != nil {
				t.Fatalf("Compile(%q): %v", c.expr, err)
			}
			if _, err := policy.Evaluate(ctx, map[string]any{}); err == nil {
				t.Fatalf("Evaluate(%q) error = nil, want a non-nil validation error", c.expr)
			}
		})
	}
}

// TestCompile_MemberCallTargetUnsupported exercises validateFunctionCalls'
// non-identifier-callee rejection through the public API: "a.b()" parses
// successfully (expr-lang/expr supports method-call syntax generally) into
// a CallNode whose Callee is a *ast.MemberNode, which grpolicy's own
// evaluator doesn't support (only a bare identifier callee, e.g. "f()").
func TestCompile_MemberCallTargetUnsupported(t *testing.T) {
	engine := newTestEngine(t)

	_, err := engine.Compile("a.b()")
	if !errors.Is(err, grpolicy.ErrCompileFailed) {
		t.Fatalf("Compile(a.b()) error = %v, want ErrCompileFailed", err)
	}
}

// TestComplexity_MemberChainNodesAreCounted proves Stage 1 of the
// member-access feature (docs/plan/member-access-support-plan.md) actually
// fixes the latent complexity-undercounting gap: before children() knew
// about *ast.MemberNode, a dotted chain of any depth counted as exactly one
// opaque leaf node, so WithMaxComplexity's node-count limit silently didn't
// apply to member-access depth. "a.b.c.d.e" is 5 links — each contributing
// both a MemberNode and a StringNode property to the count — which must now
// exceed a limit of 3 total nodes.
func TestComplexity_MemberChainNodesAreCounted(t *testing.T) {
	engine, err := grpolicy.NewEngine(grpolicy.WithMaxComplexity(3, 100))
	if err != nil {
		t.Fatalf("NewEngine(WithMaxComplexity): %v", err)
	}
	defer engine.Close()

	_, err = engine.Compile("a.b.c.d.e")
	if !errors.Is(err, grpolicy.ErrTooComplex) {
		t.Fatalf("Compile(a.b.c.d.e) error = %v, want ErrTooComplex", err)
	}
}

// TestEval_UnaryMinusUnsupported exercises evalUnary's default case: "-x"
// parses as a valid *ast.UnaryNode (expr-lang/expr supports unary minus
// generally), but grpolicy's evaluator only implements "!"/"not" — there is
// no compile-time validation of unary operators, so this is only caught at
// Evaluate time, symmetric with TestEval_UnsupportedBinaryOperatorAtEvaluate.
func TestEval_UnaryMinusUnsupported(t *testing.T) {
	engine := newTestEngine(t)
	ctx := t.Context()

	policy, err := engine.Compile("-x")
	if err != nil {
		t.Fatalf("Compile(-x): %v", err)
	}
	if _, err := policy.Evaluate(ctx, map[string]any{"x": 5}); err == nil {
		t.Fatalf("Evaluate(-x) error = nil, want a non-nil unsupported-unary-operator error")
	}
}

// TestTypeCoercion_Uint16 fills the one numeric Go kind toFloat supports
// that testTypeCoercionMatrix's cases never happen to use.
func TestTypeCoercion_Uint16(t *testing.T) {
	engine := newTestEngine(t)
	ctx := t.Context()

	policy, err := engine.Compile("x == y")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	decision, err := policy.Evaluate(ctx, map[string]any{"x": uint16(5), "y": 5})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("Allowed = false, want true (uint16 numeric coercion)")
	}
}

// TestEval_UnsupportedNodeKind exercises eval's own default case: a
// ternary parses successfully into a *ast.ConditionalNode (expr-lang/expr
// supports the general expression grammar broadly; grpolicy only
// implements a specific subset of node kinds in its own evaluator's type
// switch), so this is only ever caught at Evaluate time, not Compile time.
func TestEval_UnsupportedNodeKind(t *testing.T) {
	engine := newTestEngine(t)
	ctx := t.Context()

	policy, err := engine.Compile("a ? b : c")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if _, err := policy.Evaluate(ctx, map[string]any{"a": true, "b": 1, "c": 2}); err == nil {
		t.Fatalf("Evaluate(a ? b : c) error = nil, want a non-nil unsupported-node error")
	}
}

// TestExprText_SpacedCallSyntaxAndEscapedQuoteInString exercises
// nodeSpan/matchClosingFrom's whitespace-skip loop (a space between a
// function name and its opening paren) and its escaped-quote handling
// inside a string-literal argument — neither is exercised by any call
// syntax used elsewhere in this suite, which always writes calls with no
// space and no escaped quotes.
func TestExprText_SpacedCallSyntaxAndEscapedQuoteInString(t *testing.T) {
	engine := newTestEngine(t)
	ctx := t.Context()

	if err := engine.RegisterFunction("double", func(x int) int { return x * 2 }); err != nil {
		t.Fatalf("RegisterFunction(double): %v", err)
	}
	if err := engine.RegisterFunction("contains2", func(a, b string) bool { return true }); err != nil {
		t.Fatalf("RegisterFunction(contains2): %v", err)
	}

	t.Run("SpaceBeforeOpeningParen", func(t *testing.T) {
		policy, err := engine.Compile(`double (x) == 10`)
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		decision, err := policy.Evaluate(ctx, map[string]any{"x": 5})
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if got, want := decision.Trace[0].Children[0].Expression, "double (x)"; got != want {
			t.Fatalf("Trace child Expression = %q, want %q", got, want)
		}
	})

	t.Run("EscapedQuoteInsideStringArgument", func(t *testing.T) {
		expr := `contains2(x, "say \"hi\" now") == true`
		policy, err := engine.Compile(expr)
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		decision, err := policy.Evaluate(ctx, map[string]any{"x": "a"})
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		want := `contains2(x, "say \"hi\" now")`
		if got := decision.Trace[0].Children[0].Expression; got != want {
			t.Fatalf("Trace child Expression = %q, want %q (bracket matcher must skip the escaped quote, not treat it as the string's end)", got, want)
		}
	})
}

// TestBuiltinFunctions_BeforeAfterAddDuration invokes grpolicy's three
// hardcoded time built-ins ("before", "after", "addDuration") — they are
// registered by newFunctionRegistry at construction but no existing
// scenario ever actually calls any of them.
func TestBuiltinFunctions_BeforeAfterAddDuration(t *testing.T) {
	engine := newTestEngine(t)
	ctx := t.Context()

	t1 := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)

	cases := []struct {
		name  string
		expr  string
		attrs map[string]any
	}{
		{"Before", "before(a, b) == true", map[string]any{"a": t1, "b": t2}},
		{"After", "after(a, b) == true", map[string]any{"a": t2, "b": t1}},
		{"AddDuration", "addDuration(a, duration(\"1h\")) == b", map[string]any{"a": t1, "b": t1.Add(time.Hour)}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			policy, err := engine.Compile(c.expr)
			if err != nil {
				t.Fatalf("Compile(%q): %v", c.expr, err)
			}
			decision, err := policy.Evaluate(ctx, c.attrs)
			if err != nil {
				t.Fatalf("Evaluate(%q): %v", c.expr, err)
			}
			if !decision.Allowed {
				t.Fatalf("Evaluate(%q) Allowed = false, want true", c.expr)
			}
		})
	}
}
