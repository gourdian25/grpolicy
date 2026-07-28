// File: contract_engine_test.go

// contract_engine_test.go is grpolicy's own primary behavioral test suite,
// run via TestEngine_Contract below against grpolicy.NewEngine. grpolicy
// has exactly one Engine implementation — there is no backend-parity
// concern to prove, unlike grcache/graudit's own conformance/ packages —
// so runEngineContract is simply the reusable entry point for grpolicy's
// full scenario list, folded directly into the root package's own tests
// (matching the rest of the gourdian ecosystem's convention) rather than
// kept as a separately importable package.
package grpolicy_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gourdian25/grpolicy"
)

// runEngineContract executes grpolicy's full behavioral suite against a
// fresh Engine obtained from newEngine for each scenario. See
// TestEngine_Contract below for the entry point that drives it against
// grpolicy.NewEngine.
func runEngineContract(t *testing.T, newEngine func() (grpolicy.Engine, error)) {
	t.Helper()

	t.Run("BooleanOperators", func(t *testing.T) { testBooleanOperators(t, newEngine) })
	t.Run("ComparisonOperators", func(t *testing.T) { testComparisonOperators(t, newEngine) })
	t.Run("StringOperators", func(t *testing.T) { testStringOperators(t, newEngine) })
	t.Run("RegexMatches", func(t *testing.T) { testRegexMatches(t, newEngine) })
	t.Run("MembershipIn", func(t *testing.T) { testMembershipIn(t, newEngine) })
	t.Run("ShortCircuitAndTrace", func(t *testing.T) { testShortCircuitAndTrace(t, newEngine) })
	t.Run("MissingAttributeKey", func(t *testing.T) { testMissingAttributeKey(t, newEngine) })
	t.Run("TypeMismatch", func(t *testing.T) { testTypeMismatch(t, newEngine) })
	t.Run("ComplexityLimitRejectedAtCompile", func(t *testing.T) { testComplexityLimitRejectedAtCompile(t, newEngine) })
	t.Run("EvaluationTimeout", func(t *testing.T) { testEvaluationTimeout(t, newEngine) })
	t.Run("CustomFunctionRegistrationAndInvocation", func(t *testing.T) { testCustomFunctionRegistrationAndInvocation(t, newEngine) })
	t.Run("ConcurrentCompileEvaluateStress", func(t *testing.T) { testConcurrentCompileEvaluateStress(t, newEngine) })
	t.Run("PostClose", func(t *testing.T) { testPostClose(t, newEngine) })
	t.Run("FunctionRegistryEdgeCases", func(t *testing.T) { testFunctionRegistryEdgeCases(t, newEngine) })
	t.Run("ReDoSTimeoutBackstop", func(t *testing.T) { testReDoSTimeoutBackstop(t, newEngine) })
	t.Run("TypeCoercionMatrix", func(t *testing.T) { testTypeCoercionMatrix(t, newEngine) })
	t.Run("TraceCompletenessPerOperator", func(t *testing.T) { testTraceCompletenessPerOperator(t, newEngine) })
	t.Run("CacheKeyIsRawSourceString", func(t *testing.T) { testCacheKeyIsRawSourceString(t, newEngine) })
	t.Run("EngineOptionsAndLogger", func(t *testing.T) { testEngineOptionsAndLogger(t) })
	t.Run("FunctionCallDispatchEdgeCases", func(t *testing.T) { testFunctionCallDispatchEdgeCases(t, newEngine) })
	t.Run("ExpressionTextIncludesClosingDelimiters", func(t *testing.T) { testExpressionTextIncludesClosingDelimiters(t, newEngine) })
	t.Run("MemberAccessCollections", func(t *testing.T) { testMemberAccessCollections(t, newEngine) })
	t.Run("MemberAccessStructFields", func(t *testing.T) { testMemberAccessStructFields(t, newEngine) })
	t.Run("OptionalChainShortCircuit", func(t *testing.T) { testOptionalChainShortCircuit(t, newEngine) })
}

func testBooleanOperators(t *testing.T, newEngine func() (grpolicy.Engine, error)) {
	t.Helper()
	engine, err := newEngine()
	if err != nil {
		t.Fatalf("newEngine: %v", err)
	}
	defer engine.Close()
	ctx := context.Background()

	cases := []struct {
		expr  string
		attrs map[string]any
		want  bool
	}{
		{"a && b", map[string]any{"a": true, "b": true}, true},
		{"a && b", map[string]any{"a": true, "b": false}, false},
		{"a || b", map[string]any{"a": false, "b": true}, true},
		{"a || b", map[string]any{"a": false, "b": false}, false},
		{"!a", map[string]any{"a": false}, true},
		{"not a", map[string]any{"a": true}, false},
		{"a && b || c", map[string]any{"a": false, "b": true, "c": true}, true},
	}
	for _, c := range cases {
		policy, err := engine.Compile(c.expr)
		if err != nil {
			t.Fatalf("Compile(%q): %v", c.expr, err)
		}
		decision, err := policy.Evaluate(ctx, c.attrs)
		if err != nil {
			t.Fatalf("Evaluate(%q, %v): %v", c.expr, c.attrs, err)
		}
		if decision.Allowed != c.want {
			t.Fatalf("Evaluate(%q, %v) = %v, want %v", c.expr, c.attrs, decision.Allowed, c.want)
		}
	}
}

func testComparisonOperators(t *testing.T, newEngine func() (grpolicy.Engine, error)) {
	t.Helper()
	engine, err := newEngine()
	if err != nil {
		t.Fatalf("newEngine: %v", err)
	}
	defer engine.Close()
	ctx := context.Background()

	cases := []struct {
		expr  string
		attrs map[string]any
		want  bool
	}{
		{"age == 18", map[string]any{"age": 18}, true},
		{"age != 18", map[string]any{"age": 21}, true},
		{"age < 18", map[string]any{"age": 17}, true},
		{"age <= 18", map[string]any{"age": 18}, true},
		{"age > 18", map[string]any{"age": 19}, true},
		{"age >= 18", map[string]any{"age": 18}, true},
		{"age >= 18", map[string]any{"age": 17}, false},
		// numeric normalization: int64/float64 attribute vs int literal
		{"score >= 18", map[string]any{"score": float64(18.5)}, true},
		{"score >= 18", map[string]any{"score": int64(17)}, false},
	}
	for _, c := range cases {
		policy, err := engine.Compile(c.expr)
		if err != nil {
			t.Fatalf("Compile(%q): %v", c.expr, err)
		}
		decision, err := policy.Evaluate(ctx, c.attrs)
		if err != nil {
			t.Fatalf("Evaluate(%q, %v): %v", c.expr, c.attrs, err)
		}
		if decision.Allowed != c.want {
			t.Fatalf("Evaluate(%q, %v) = %v, want %v", c.expr, c.attrs, decision.Allowed, c.want)
		}
	}
}

func testStringOperators(t *testing.T, newEngine func() (grpolicy.Engine, error)) {
	t.Helper()
	engine, err := newEngine()
	if err != nil {
		t.Fatalf("newEngine: %v", err)
	}
	defer engine.Close()
	ctx := context.Background()

	cases := []struct {
		expr  string
		attrs map[string]any
		want  bool
	}{
		{`name contains "Smith"`, map[string]any{"name": "John Smith"}, true},
		{`name contains "jones"`, map[string]any{"name": "John Smith"}, false},
		{`name startsWith "John"`, map[string]any{"name": "John Smith"}, true},
		{`name endsWith "Smith"`, map[string]any{"name": "John Smith"}, true},
	}
	for _, c := range cases {
		policy, err := engine.Compile(c.expr)
		if err != nil {
			t.Fatalf("Compile(%q): %v", c.expr, err)
		}
		decision, err := policy.Evaluate(ctx, c.attrs)
		if err != nil {
			t.Fatalf("Evaluate(%q, %v): %v", c.expr, c.attrs, err)
		}
		if decision.Allowed != c.want {
			t.Fatalf("Evaluate(%q, %v) = %v, want %v", c.expr, c.attrs, decision.Allowed, c.want)
		}
	}
}

func testRegexMatches(t *testing.T, newEngine func() (grpolicy.Engine, error)) {
	t.Helper()
	engine, err := newEngine()
	if err != nil {
		t.Fatalf("newEngine: %v", err)
	}
	defer engine.Close()
	ctx := context.Background()

	policy, err := engine.Compile(`name matches "^A.*"`)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if d, err := policy.Evaluate(ctx, map[string]any{"name": "Admin"}); err != nil || !d.Allowed {
		t.Fatalf("Evaluate(Admin) = (%v, %v), want (true, nil)", d.Allowed, err)
	}
	if d, err := policy.Evaluate(ctx, map[string]any{"name": "bob"}); err != nil || d.Allowed {
		t.Fatalf("Evaluate(bob) = (%v, %v), want (false, nil)", d.Allowed, err)
	}

	// ReDoS sanity check: Go's regexp package is RE2-based (linear time by
	// construction), so a classically-pathological pattern for a
	// backtracking engine must still complete quickly here.
	pathological, err := engine.Compile(`s matches "(a+)+$"`)
	if err != nil {
		t.Fatalf("Compile(pathological): %v", err)
	}
	subject := ""
	for i := 0; i < 40; i++ {
		subject += "a"
	}
	subject += "b"
	done := make(chan struct{})
	go func() {
		policy2 := pathological
		_, _ = policy2.Evaluate(ctx, map[string]any{"s": subject})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("matches against a classically-pathological pattern did not complete within 2s — RE2 linear-time guarantee violated")
	}
}

func testMembershipIn(t *testing.T, newEngine func() (grpolicy.Engine, error)) {
	t.Helper()
	engine, err := newEngine()
	if err != nil {
		t.Fatalf("newEngine: %v", err)
	}
	defer engine.Close()
	ctx := context.Background()

	policy, err := engine.Compile(`role in ["admin", "owner"]`)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if d, err := policy.Evaluate(ctx, map[string]any{"role": "admin"}); err != nil || !d.Allowed {
		t.Fatalf("Evaluate(admin) = (%v, %v), want (true, nil)", d.Allowed, err)
	}
	if d, err := policy.Evaluate(ctx, map[string]any{"role": "guest"}); err != nil || d.Allowed {
		t.Fatalf("Evaluate(guest) = (%v, %v), want (false, nil)", d.Allowed, err)
	}

	policySlice, err := engine.Compile(`role in roles`)
	if err != nil {
		t.Fatalf("Compile(roles): %v", err)
	}
	if d, err := policySlice.Evaluate(ctx, map[string]any{"role": "admin", "roles": []string{"admin", "user"}}); err != nil || !d.Allowed {
		t.Fatalf("Evaluate([]string roles) = (%v, %v), want (true, nil)", d.Allowed, err)
	}

	policyMap, err := engine.Compile(`role in permissions`)
	if err != nil {
		t.Fatalf("Compile(permissions): %v", err)
	}
	if d, err := policyMap.Evaluate(ctx, map[string]any{"role": "admin", "permissions": map[string]any{"admin": true}}); err != nil || !d.Allowed {
		t.Fatalf("Evaluate(map permissions) = (%v, %v), want (true, nil)", d.Allowed, err)
	}
}

// testShortCircuitAndTrace is the falsifying test for the short-circuit
// trace policy documented on Decision: a short-circuited &&/|| branch must
// never actually be evaluated (proven by a call counter), and Trace must
// still contain a CheckNode for it, marked Evaluated: false with a nil
// Result — never a coerced false.
func testShortCircuitAndTrace(t *testing.T, newEngine func() (grpolicy.Engine, error)) {
	t.Helper()
	engine, err := newEngine()
	if err != nil {
		t.Fatalf("newEngine: %v", err)
	}
	defer engine.Close()
	ctx := context.Background()

	var calls int32
	if err := engine.RegisterFunction("expensive", func() bool {
		atomic.AddInt32(&calls, 1)
		return true
	}); err != nil {
		t.Fatalf("RegisterFunction: %v", err)
	}

	t.Run("AndShortCircuitsOnFalse", func(t *testing.T) {
		atomic.StoreInt32(&calls, 0)
		policy, err := engine.Compile("enabled && expensive()")
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		decision, err := policy.Evaluate(ctx, map[string]any{"enabled": false})
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if decision.Allowed {
			t.Fatalf("Allowed = true, want false")
		}
		if got := atomic.LoadInt32(&calls); got != 0 {
			t.Fatalf("expensive() was called %d times, want 0 — short-circuit did not actually prevent evaluation", got)
		}
		assertUnevaluatedRightBranch(t, decision)
	})

	t.Run("OrShortCircuitsOnTrue", func(t *testing.T) {
		atomic.StoreInt32(&calls, 0)
		policy, err := engine.Compile("enabled || expensive()")
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		decision, err := policy.Evaluate(ctx, map[string]any{"enabled": true})
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if !decision.Allowed {
			t.Fatalf("Allowed = false, want true")
		}
		if got := atomic.LoadInt32(&calls); got != 0 {
			t.Fatalf("expensive() was called %d times, want 0 — short-circuit did not actually prevent evaluation", got)
		}
		assertUnevaluatedRightBranch(t, decision)
	})
}

func assertUnevaluatedRightBranch(t *testing.T, decision grpolicy.Decision) {
	t.Helper()
	if len(decision.Trace) != 1 {
		t.Fatalf("Trace has %d root nodes, want 1", len(decision.Trace))
	}
	root := decision.Trace[0]
	if len(root.Children) != 2 {
		t.Fatalf("root.Children has %d entries, want 2", len(root.Children))
	}
	right := root.Children[1]
	if right.Evaluated {
		t.Fatalf("right branch CheckNode.Evaluated = true, want false (it must never have run)")
	}
	if right.Result != nil {
		t.Fatalf("right branch CheckNode.Result = %#v, want nil — an unevaluated node's Result must never be a coerced false", right.Result)
	}
}

// testMissingAttributeKey is the falsifying test for the fail-loud missing-
// key policy: Evaluate must return ErrAttributeMissing (not silently
// substitute a zero value), and Decision.Trace must still contain the
// partial tree built up to the point of failure.
func testMissingAttributeKey(t *testing.T, newEngine func() (grpolicy.Engine, error)) {
	t.Helper()
	engine, err := newEngine()
	if err != nil {
		t.Fatalf("newEngine: %v", err)
	}
	defer engine.Close()
	ctx := context.Background()

	policy, err := engine.Compile("age >= 18")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	decision, err := policy.Evaluate(ctx, map[string]any{})
	if !errors.Is(err, grpolicy.ErrAttributeMissing) {
		t.Fatalf("Evaluate error = %v, want ErrAttributeMissing", err)
	}
	if decision.Allowed {
		t.Fatalf("Allowed = true on error, want false")
	}
	if len(decision.Trace) != 1 {
		t.Fatalf("Trace has %d root nodes, want 1 (partial trace must still be populated)", len(decision.Trace))
	}
	root := decision.Trace[0]
	if len(root.Children) != 2 {
		t.Fatalf("root.Children has %d entries, want 2", len(root.Children))
	}
	missing := root.Children[0]
	if missing.Expression != "age" {
		t.Fatalf("root.Children[0].Expression = %q, want %q", missing.Expression, "age")
	}
	if missing.Evaluated {
		t.Fatalf("missing-attribute CheckNode.Evaluated = true, want false")
	}
}

// testTypeMismatch is the falsifying test for the fail-loud type-mismatch
// policy: a string attribute compared against a numeric literal must fail
// with ErrTypeMismatch, and Trace must show both operands as successfully
// evaluated (they were) with only the comparison operator itself left
// unevaluated.
func testTypeMismatch(t *testing.T, newEngine func() (grpolicy.Engine, error)) {
	t.Helper()
	engine, err := newEngine()
	if err != nil {
		t.Fatalf("newEngine: %v", err)
	}
	defer engine.Close()
	ctx := context.Background()

	policy, err := engine.Compile("age < 18")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	decision, err := policy.Evaluate(ctx, map[string]any{"age": "twenty"})
	if !errors.Is(err, grpolicy.ErrTypeMismatch) {
		t.Fatalf("Evaluate error = %v, want ErrTypeMismatch", err)
	}
	if len(decision.Trace) != 1 {
		t.Fatalf("Trace has %d root nodes, want 1", len(decision.Trace))
	}
	root := decision.Trace[0]
	if root.Evaluated {
		t.Fatalf("root CheckNode.Evaluated = true, want false (the comparison itself failed)")
	}
	if len(root.Children) != 2 {
		t.Fatalf("root.Children has %d entries, want 2", len(root.Children))
	}
	if !root.Children[0].Evaluated || !root.Children[1].Evaluated {
		t.Fatalf("both operands should show Evaluated: true — they ran successfully; only the comparison operator failed")
	}
}

func testComplexityLimitRejectedAtCompile(t *testing.T, newEngine func() (grpolicy.Engine, error)) {
	t.Helper()
	base, err := newEngine()
	if err != nil {
		t.Fatalf("newEngine: %v", err)
	}
	defer base.Close()
	_ = base

	engine, err := grpolicy.NewEngine(grpolicy.WithMaxComplexity(10, 100))
	if err != nil {
		t.Fatalf("NewEngine(WithMaxComplexity): %v", err)
	}
	defer engine.Close()

	// 20 "a == 1" terms joined by && comfortably exceeds a 10-node budget.
	expr := "a == 1"
	for i := 0; i < 20; i++ {
		expr += " && a == 1"
	}
	_, err = engine.Compile(expr)
	if !errors.Is(err, grpolicy.ErrTooComplex) {
		t.Fatalf("Compile(oversized expr) error = %v, want ErrTooComplex", err)
	}
}

func testEvaluationTimeout(t *testing.T, newEngine func() (grpolicy.Engine, error)) {
	t.Helper()
	engine, err := newEngine()
	if err != nil {
		t.Fatalf("newEngine: %v", err)
	}
	defer engine.Close()

	if err := engine.RegisterFunction("slow", func() bool {
		time.Sleep(60 * time.Millisecond)
		return true
	}); err != nil {
		t.Fatalf("RegisterFunction: %v", err)
	}

	// slow() && slow(): the first call is allowed to complete (grpolicy
	// cannot preempt an in-flight reflected call), but the ctx check at the
	// top of eval() for the second slow() call must catch the expired
	// deadline before invoking it again — proving cancellation is checked
	// between sub-expressions, not just once at the start of Evaluate.
	policy, err := engine.Compile("slow() && slow()")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err = policy.Evaluate(ctx, map[string]any{})
	elapsed := time.Since(start)

	if !errors.Is(err, grpolicy.ErrEvaluationTimeout) {
		t.Fatalf("Evaluate error = %v, want ErrEvaluationTimeout", err)
	}
	if elapsed >= 110*time.Millisecond {
		t.Fatalf("Evaluate took %v, want well under ~120ms (two slow() calls, proving the second call was never actually invoked)", elapsed)
	}
}

func testCustomFunctionRegistrationAndInvocation(t *testing.T, newEngine func() (grpolicy.Engine, error)) {
	t.Helper()
	engine, err := newEngine()
	if err != nil {
		t.Fatalf("newEngine: %v", err)
	}
	defer engine.Close()
	ctx := context.Background()

	if err := engine.RegisterFunction("double", func(x int) int { return x * 2 }); err != nil {
		t.Fatalf("RegisterFunction: %v", err)
	}

	policy, err := engine.Compile("double(21) == 42")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	decision, err := policy.Evaluate(ctx, map[string]any{})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("Allowed = false, want true")
	}

	_, err = engine.Compile("notRegistered()")
	if !errors.Is(err, grpolicy.ErrUnknownFunction) {
		t.Fatalf("Compile(unregistered function) error = %v, want ErrUnknownFunction", err)
	}
}

// testConcurrentCompileEvaluateStress is the proof (not assumption) that the
// compiled-policy cache is safe under real concurrent load: many goroutines
// Compile overlapping and distinct expressions while other goroutines
// concurrently Evaluate already-compiled policies. Must be run with
// `go test -race` — a pass without -race proves nothing.
func testConcurrentCompileEvaluateStress(t *testing.T, newEngine func() (grpolicy.Engine, error)) {
	t.Helper()
	engine, err := newEngine()
	if err != nil {
		t.Fatalf("newEngine: %v", err)
	}
	defer engine.Close()
	ctx := context.Background()

	exprs := make([]string, 10)
	for i := range exprs {
		exprs[i] = fmt.Sprintf("x >= %d", i)
	}

	const compilers = 20
	const evaluators = 20
	const opsPerWorker = 50

	var wg sync.WaitGroup
	errCh := make(chan error, compilers+evaluators)

	wg.Add(compilers)
	for w := 0; w < compilers; w++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < opsPerWorker; i++ {
				expr := exprs[(id+i)%len(exprs)]
				policy, err := engine.Compile(expr)
				if err != nil {
					errCh <- fmt.Errorf("Compile(%q): %w", expr, err)
					return
				}
				if _, err := policy.Evaluate(ctx, map[string]any{"x": 100}); err != nil {
					errCh <- fmt.Errorf("evaluate after Compile(%q): %w", expr, err)
					return
				}
			}
		}(w)
	}

	// Pre-compile a couple of policies so evaluator goroutines have
	// something to hammer on from the start, not just whatever compilers
	// happen to have produced yet.
	seed, err := engine.Compile(exprs[0])
	if err != nil {
		t.Fatalf("Compile(seed): %v", err)
	}

	wg.Add(evaluators)
	for w := 0; w < evaluators; w++ {
		go func() {
			defer wg.Done()
			for i := 0; i < opsPerWorker; i++ {
				decision, err := seed.Evaluate(ctx, map[string]any{"x": -1})
				if err != nil {
					errCh <- fmt.Errorf("Evaluate(seed): %w", err)
					return
				}
				if decision.Allowed {
					errCh <- fmt.Errorf("Evaluate(seed) with x=-1 = Allowed, want denied")
					return
				}
			}
		}()
	}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("concurrent stress: %v", err)
	}
}

func testPostClose(t *testing.T, newEngine func() (grpolicy.Engine, error)) {
	t.Helper()
	engine, err := newEngine()
	if err != nil {
		t.Fatalf("newEngine: %v", err)
	}

	if _, err := engine.Compile("a == 1"); err != nil {
		t.Fatalf("Compile (before close): %v", err)
	}

	if err := engine.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := engine.Close(); err != nil {
		t.Fatalf("second Close: %v, want nil (idempotent)", err)
	}

	if _, err := engine.Compile("a == 1"); !errors.Is(err, grpolicy.ErrClosed) {
		t.Fatalf("Compile after Close error = %v, want ErrClosed", err)
	}
	if err := engine.RegisterFunction("f", func() {}); !errors.Is(err, grpolicy.ErrClosed) {
		t.Fatalf("RegisterFunction after Close error = %v, want ErrClosed", err)
	}
}

// testFunctionRegistryEdgeCases covers three behaviors of the custom
// function registry that the two named falsifying tests (ShortCircuit,
// MissingAttributeKey) don't touch: rejecting registrations that collide
// with expr-lang/expr's own reserved builtin identifiers (a call to such a
// name can never reach a registered function — see docs/architecture.md
// §9), the deliberate last-write-wins behavior of registering the same name
// twice, and panic recovery for a custom function that panics mid-call —
// the one other place caller-supplied code executes, which needs the same
// recover() treatment grevents' own audit required for Logger/
// DeadLetterSink call sites.
func testFunctionRegistryEdgeCases(t *testing.T, newEngine func() (grpolicy.Engine, error)) {
	t.Helper()
	engine, err := newEngine()
	if err != nil {
		t.Fatalf("newEngine: %v", err)
	}
	defer engine.Close()
	ctx := context.Background()

	t.Run("ReservedBuiltinNamesRejected", func(t *testing.T) {
		// "now" and "duration" are the two grpolicy itself implements as
		// BuiltinNode handlers; "len" and "all" are reserved by
		// expr-lang/expr but not implemented by grpolicy at all — both
		// classes must be rejected at registration time.
		for _, name := range []string{"now", "duration", "len", "all"} {
			if err := engine.RegisterFunction(name, func() bool { return true }); err == nil {
				t.Fatalf("RegisterFunction(%q) = nil error, want a rejection (name collides with an expr-lang/expr reserved builtin identifier)", name)
			}
		}
	})

	t.Run("DoubleRegistrationIsLastWriteWins", func(t *testing.T) {
		if err := engine.RegisterFunction("double", func(x int) int { return x * 2 }); err != nil {
			t.Fatalf("RegisterFunction (first): %v", err)
		}
		if err := engine.RegisterFunction("double", func(x int) int { return x * 3 }); err != nil {
			t.Fatalf("RegisterFunction (second, overwriting): %v", err)
		}
		policy, err := engine.Compile("double(2) == 6")
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		decision, err := policy.Evaluate(ctx, map[string]any{})
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if !decision.Allowed {
			t.Fatalf("Allowed = false, want true — second registration (x*3) should have overwritten the first (x*2): double(2) should be 6, not 4")
		}
	})

	t.Run("CustomFunctionPanicIsRecoveredNotCrashed", func(t *testing.T) {
		if err := engine.RegisterFunction("boom", func() bool { panic("deliberate panic for test coverage") }); err != nil {
			t.Fatalf("RegisterFunction: %v", err)
		}
		policy, err := engine.Compile("boom()")
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		// If the panic weren't recovered, this call would crash the whole
		// test binary rather than returning — reaching the assertion below
		// at all is itself part of what this test proves.
		_, err = policy.Evaluate(ctx, map[string]any{})
		if err == nil {
			t.Fatalf("Evaluate error = nil, want a non-nil error surfaced from the recovered panic")
		}
	})
}

// testReDoSTimeoutBackstop proves the ctx-deadline check is a genuine
// second line of defense, not merely relying on RE2's linear-time
// guarantee as the only protection: a slow custom function combined with a
// matches operator in the same expression must have the matches operator
// skipped entirely once ctx expires, before the regex engine is ever
// invoked for it.
func testReDoSTimeoutBackstop(t *testing.T, newEngine func() (grpolicy.Engine, error)) {
	t.Helper()
	engine, err := newEngine()
	if err != nil {
		t.Fatalf("newEngine: %v", err)
	}
	defer engine.Close()

	if err := engine.RegisterFunction("slow", func() bool {
		time.Sleep(60 * time.Millisecond)
		return true
	}); err != nil {
		t.Fatalf("RegisterFunction: %v", err)
	}

	policy, err := engine.Compile(`slow() && name matches "(a+)+$"`)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	longSubject := ""
	for i := 0; i < 35; i++ {
		longSubject += "a"
	}
	longSubject += "!"

	start := time.Now()
	_, err = policy.Evaluate(ctx, map[string]any{"name": longSubject})
	elapsed := time.Since(start)

	if !errors.Is(err, grpolicy.ErrEvaluationTimeout) {
		t.Fatalf("Evaluate error = %v, want ErrEvaluationTimeout", err)
	}
	if elapsed >= 100*time.Millisecond {
		t.Fatalf("Evaluate took %v, want well under 100ms — the matches operator must never have been reached once ctx expired after slow()", elapsed)
	}
}

// typeCoercionCase is one cell of the type-coercion matrix: an operator
// applied to a specific pair of attribute types, with an explicit expected
// outcome — either a specific boolean result, or a specific sentinel error.
// No cell is left implicit.
type typeCoercionCase struct {
	name       string
	expr       string
	attrs      map[string]any
	wantResult bool  // only checked when wantErr == nil
	wantErr    error // nil means "expect no error"
}

// testTypeCoercionMatrix is one table-driven test covering every operator
// grpolicy supports against every type-pair category documented in
// docs/architecture.md's type rules: numeric/numeric, numeric/string,
// string/string, string/numeric, time.Time/time.Time, time.Time/numeric,
// bool/bool, bool/numeric, nil-present/nil-present, nil-present/numeric,
// and — for in/contains/startsWith/endsWith/matches specifically — their
// own operator-specific type requirements and haystack-kind coverage.
func testTypeCoercionMatrix(t *testing.T, newEngine func() (grpolicy.Engine, error)) {
	t.Helper()
	engine, err := newEngine()
	if err != nil {
		t.Fatalf("newEngine: %v", err)
	}
	defer engine.Close()
	ctx := context.Background()

	t1 := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)

	cases := []typeCoercionCase{
		// -- comparisons: numeric/numeric --
		{"IntEqInt_true", "x == y", map[string]any{"x": 5, "y": 5}, true, nil},
		{"IntEqInt_false", "x == y", map[string]any{"x": 5, "y": 3}, false, nil},
		{"IntNeInt", "x != y", map[string]any{"x": 5, "y": 3}, true, nil},
		{"IntLtFloat", "x < y", map[string]any{"x": 3, "y": 3.5}, true, nil},
		{"IntLeFloat_equalValue", "x <= y", map[string]any{"x": 3, "y": float64(3)}, true, nil},
		{"IntGtInt64", "x > y", map[string]any{"x": 5, "y": int64(3)}, true, nil},
		{"IntGeUint", "x >= y", map[string]any{"x": 5, "y": uint(5)}, true, nil},
		{"Int8EqInt16", "x == y", map[string]any{"x": int8(5), "y": int16(5)}, true, nil},
		{"Int32EqUint32", "x == y", map[string]any{"x": int32(5), "y": uint32(5)}, true, nil},
		{"Uint8EqUint64", "x == y", map[string]any{"x": uint8(5), "y": uint64(5)}, true, nil},
		{"Float32EqFloat64", "x == y", map[string]any{"x": float32(5), "y": float64(5)}, true, nil},

		// -- comparisons: string/string --
		{"StringEqString_true", "x == y", map[string]any{"x": "abc", "y": "abc"}, true, nil},
		{"StringLtString", "x < y", map[string]any{"x": "abc", "y": "abd"}, true, nil},
		{"StringGeString_false", "x >= y", map[string]any{"x": "abc", "y": "abd"}, false, nil},
		{"StringNeString_true", "x != y", map[string]any{"x": "abc", "y": "abd"}, true, nil},
		{"StringLeString_true", "x <= y", map[string]any{"x": "abc", "y": "abc"}, true, nil},
		{"StringGtString_true", "x > y", map[string]any{"x": "abd", "y": "abc"}, true, nil},

		// -- comparisons: numeric/string mismatch (every operator) --
		{"IntEqString_mismatch", "x == y", map[string]any{"x": 5, "y": "5"}, false, grpolicy.ErrTypeMismatch},
		{"IntNeString_mismatch", "x != y", map[string]any{"x": 5, "y": "5"}, false, grpolicy.ErrTypeMismatch},
		{"StringLtInt_mismatch", "x < y", map[string]any{"x": "5", "y": 5}, false, grpolicy.ErrTypeMismatch},
		{"StringLeInt_mismatch", "x <= y", map[string]any{"x": "5", "y": 5}, false, grpolicy.ErrTypeMismatch},
		{"StringGtInt_mismatch", "x > y", map[string]any{"x": "5", "y": 5}, false, grpolicy.ErrTypeMismatch},
		{"StringGeInt_mismatch", "x >= y", map[string]any{"x": "5", "y": 5}, false, grpolicy.ErrTypeMismatch},

		// -- comparisons: time.Time/time.Time --
		{"TimeEqTime_false", "x == y", map[string]any{"x": t1, "y": t2}, false, nil},
		{"TimeLtTime_true", "x < y", map[string]any{"x": t1, "y": t2}, true, nil},
		{"TimeGeTime_false", "x >= y", map[string]any{"x": t1, "y": t2}, false, nil},
		{"TimeLeTime_equal", "x <= y", map[string]any{"x": t1, "y": t1}, true, nil},
		{"TimeNeTime_true", "x != y", map[string]any{"x": t1, "y": t2}, true, nil},
		{"TimeGtTime_true", "x > y", map[string]any{"x": t2, "y": t1}, true, nil},

		// -- comparisons: time.Time/numeric mismatch --
		{"TimeEqInt_mismatch", "x == y", map[string]any{"x": t1, "y": 5}, false, grpolicy.ErrTypeMismatch},
		{"TimeLtInt_mismatch", "x < y", map[string]any{"x": t1, "y": 5}, false, grpolicy.ErrTypeMismatch},

		// -- comparisons: bool/bool --
		{"BoolEqBool_true", "x == y", map[string]any{"x": true, "y": true}, true, nil},
		{"BoolNeBool_true", "x != y", map[string]any{"x": true, "y": false}, true, nil},
		{"BoolLtBool_unsupportedOp", "x < y", map[string]any{"x": true, "y": false}, false, grpolicy.ErrTypeMismatch},

		// -- comparisons: bool/numeric mismatch --
		{"BoolEqInt_mismatch", "x == y", map[string]any{"x": true, "y": 1}, false, grpolicy.ErrTypeMismatch},

		// -- comparisons: nil-present (not missing) --
		{"NilEqNil_true", "x == y", map[string]any{"x": nil, "y": nil}, true, nil},
		{"NilNeInt_true", "x != y", map[string]any{"x": nil, "y": 5}, true, nil},
		{"NilLtInt_mismatch", "x < y", map[string]any{"x": nil, "y": 5}, false, grpolicy.ErrTypeMismatch},

		// -- comparisons: missing attribute key --
		{"MissingKeyLeft_eq", "x == y", map[string]any{"y": 5}, false, grpolicy.ErrAttributeMissing},
		{"MissingKeyLeft_contains", `x contains "a"`, map[string]any{}, false, grpolicy.ErrAttributeMissing},
		{"MissingKeyLeft_in", "x in y", map[string]any{"y": []string{"a"}}, false, grpolicy.ErrAttributeMissing},
		{"MissingKeyLeft_matches", `x matches "a"`, map[string]any{}, false, grpolicy.ErrAttributeMissing},

		// -- string operators: string/string --
		{"Contains_true", `x contains "bc"`, map[string]any{"x": "abcd"}, true, nil},
		{"StartsWith_false", `x startsWith "bc"`, map[string]any{"x": "abcd"}, false, nil},
		{"EndsWith_true", `x endsWith "cd"`, map[string]any{"x": "abcd"}, true, nil},

		// -- string operators: type mismatch --
		{"Contains_intSubject_mismatch", `x contains "a"`, map[string]any{"x": 5}, false, grpolicy.ErrTypeMismatch},
		{"StartsWith_intPattern_mismatch", "x startsWith y", map[string]any{"x": "abc", "y": 5}, false, grpolicy.ErrTypeMismatch},

		// -- matches: string/string, valid and invalid pattern --
		{"Matches_true", `x matches "^a.*d$"`, map[string]any{"x": "abcd"}, true, nil},
		{"Matches_false", `x matches "^z.*"`, map[string]any{"x": "abcd"}, false, nil},
		{"Matches_intSubject_mismatch", `x matches "a"`, map[string]any{"x": 5}, false, grpolicy.ErrTypeMismatch},
		{"Matches_intPattern_mismatch", "x matches y", map[string]any{"x": "abc", "y": 5}, false, grpolicy.ErrTypeMismatch},
		{"Matches_invalidRegex_compileFailed", "x matches y", map[string]any{"x": "abc", "y": "["}, false, grpolicy.ErrCompileFailed},

		// -- in: slice, map, unsupported haystack, nil haystack --
		{"In_sliceString_found", `x in y`, map[string]any{"x": "b", "y": []string{"a", "b"}}, true, nil},
		{"In_sliceString_notFound", `x in y`, map[string]any{"x": "z", "y": []string{"a", "b"}}, false, nil},
		{"In_sliceInt_found", `x in y`, map[string]any{"x": 2, "y": []int{1, 2, 3}}, true, nil},
		{"In_mapKeys_found", `x in y`, map[string]any{"x": "a", "y": map[string]any{"a": true}}, true, nil},
		{"In_mapKeys_notFound", `x in y`, map[string]any{"x": "z", "y": map[string]any{"a": true}}, false, nil},
		{"In_mapWithNonStringNeedle_mismatch", `x in y`, map[string]any{"x": 5, "y": map[string]any{"a": true}}, false, grpolicy.ErrTypeMismatch},
		{"In_unsupportedHaystack_mismatch", `x in y`, map[string]any{"x": 5, "y": 5}, false, grpolicy.ErrTypeMismatch},
		{"In_nilHaystack_false", `x in y`, map[string]any{"x": 5, "y": nil}, false, nil},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			policy, err := engine.Compile(c.expr)
			if err != nil {
				t.Fatalf("Compile(%q): %v", c.expr, err)
			}
			decision, err := policy.Evaluate(ctx, c.attrs)
			if c.wantErr != nil {
				if !errors.Is(err, c.wantErr) {
					t.Fatalf("Evaluate(%q, %v) error = %v, want %v", c.expr, c.attrs, err, c.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Evaluate(%q, %v): unexpected error %v", c.expr, c.attrs, err)
			}
			if decision.Allowed != c.wantResult {
				t.Fatalf("Evaluate(%q, %v) = %v, want %v", c.expr, c.attrs, decision.Allowed, c.wantResult)
			}
		})
	}
}

// testTraceCompletenessPerOperator asserts the full CheckNode shape
// (Expression, Operator, Result, Passed, Evaluated) for one representative
// case of every supported operator individually — ShortCircuitAndTrace and
// MissingAttributeKey each prove one specific claim about the trace, but
// neither exercises comparisons, in, contains, matches, or function calls.
func testTraceCompletenessPerOperator(t *testing.T, newEngine func() (grpolicy.Engine, error)) {
	t.Helper()
	engine, err := newEngine()
	if err != nil {
		t.Fatalf("newEngine: %v", err)
	}
	defer engine.Close()
	ctx := context.Background()
	if err := engine.RegisterFunction("double", func(x int) int { return x * 2 }); err != nil {
		t.Fatalf("RegisterFunction: %v", err)
	}

	cases := []struct {
		name     string
		expr     string
		attrs    map[string]any
		wantOp   string
		wantPass bool
	}{
		{"And", "a && b", map[string]any{"a": true, "b": true}, "&&", true},
		{"Or", "a || b", map[string]any{"a": false, "b": true}, "||", true},
		{"Not", "!a", map[string]any{"a": false}, "!", true},
		{"Eq", "a == b", map[string]any{"a": 1, "b": 1}, "==", true},
		{"Lt", "a < b", map[string]any{"a": 1, "b": 2}, "<", true},
		{"Contains", `a contains "x"`, map[string]any{"a": "xyz"}, "contains", true},
		{"StartsWith", `a startsWith "x"`, map[string]any{"a": "xyz"}, "startsWith", true},
		{"EndsWith", `a endsWith "z"`, map[string]any{"a": "xyz"}, "endsWith", true},
		{"Matches", `a matches "^x"`, map[string]any{"a": "xyz"}, "matches", true},
		{"In", "a in b", map[string]any{"a": "x", "b": []string{"x"}}, "in", true},
		{"Call", "double(a) == b", map[string]any{"a": 2, "b": 4}, "==", true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			policy, err := engine.Compile(c.expr)
			if err != nil {
				t.Fatalf("Compile(%q): %v", c.expr, err)
			}
			decision, err := policy.Evaluate(ctx, c.attrs)
			if err != nil {
				t.Fatalf("Evaluate(%q, %v): %v", c.expr, c.attrs, err)
			}
			if len(decision.Trace) != 1 {
				t.Fatalf("Trace has %d root nodes, want 1", len(decision.Trace))
			}
			root := decision.Trace[0]
			if root.Expression != c.expr {
				t.Fatalf("root.Expression = %q, want %q", root.Expression, c.expr)
			}
			if root.Operator != c.wantOp {
				t.Fatalf("root.Operator = %q, want %q", root.Operator, c.wantOp)
			}
			if !root.Evaluated {
				t.Fatalf("root.Evaluated = false, want true")
			}
			if root.Passed != c.wantPass {
				t.Fatalf("root.Passed = %v, want %v", root.Passed, c.wantPass)
			}
			if root.Result == nil {
				t.Fatalf("root.Result = nil, want a non-nil result for a successfully evaluated node")
			}
		})
	}
}

// testCacheKeyIsRawSourceString documents and proves the compiled-policy
// cache's key behavior: it is the exact, unnormalized source string passed
// to Compile — "a && b" and "a&&b" are semantically identical but hit two
// separate cache entries, while calling Compile twice with byte-identical
// text returns the same cached CompiledPolicy. See docs/architecture.md for
// why this was chosen over AST-based cache-key normalization.
func testCacheKeyIsRawSourceString(t *testing.T, newEngine func() (grpolicy.Engine, error)) {
	t.Helper()
	engine, err := newEngine()
	if err != nil {
		t.Fatalf("newEngine: %v", err)
	}
	defer engine.Close()

	p1, err := engine.Compile("a && b")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	p2, err := engine.Compile("a && b")
	if err != nil {
		t.Fatalf("Compile (identical text again): %v", err)
	}
	if p1 != p2 {
		t.Fatalf("Compile called twice with identical source text returned different CompiledPolicy values, want a cache hit returning the same value")
	}

	p3, err := engine.Compile("a&&b")
	if err != nil {
		t.Fatalf("Compile (whitespace-different but semantically identical): %v", err)
	}
	if p1 == p3 {
		t.Fatalf("Compile(%q) and Compile(%q) returned the same CompiledPolicy value — the cache key is documented as the raw, unnormalized source string, so these must be separate entries", "a && b", "a&&b")
	}
	if p3.Source() != "a&&b" {
		t.Fatalf("p3.Source() = %q, want %q (unnormalized)", p3.Source(), "a&&b")
	}
}

// capturingLogger records every call it receives, safe for concurrent use.
type capturingLogger struct {
	mu       sync.Mutex
	messages []string
}

func (l *capturingLogger) Debug(msg string, args ...any) { l.record(msg) }
func (l *capturingLogger) Info(msg string, args ...any)  { l.record(msg) }
func (l *capturingLogger) Warn(msg string, args ...any)  { l.record(msg) }
func (l *capturingLogger) Error(msg string, args ...any) { l.record(msg) }

func (l *capturingLogger) record(msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.messages = append(l.messages, msg)
}

func (l *capturingLogger) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.messages)
}

// testEngineOptionsAndLogger exercises every EngineOption directly — none
// of them are touched by the newEngine-factory-based scenarios above, which
// all construct a default-configured Engine. This tests grpolicy.NewEngine
// directly rather than going through the newEngine factory, since the
// factory (by design, in grpolicy_test.go) doesn't parameterize options.
func testEngineOptionsAndLogger(t *testing.T) {
	t.Helper()

	t.Run("WithLoggerReceivesDiagnosticMessages", func(t *testing.T) {
		logger := &capturingLogger{}
		engine, err := grpolicy.NewEngine(grpolicy.WithLogger(logger))
		if err != nil {
			t.Fatalf("NewEngine: %v", err)
		}
		if _, err := engine.Compile("a == 1"); err != nil {
			t.Fatalf("Compile: %v", err)
		}
		if logger.count() == 0 {
			t.Fatalf("logger recorded 0 messages after Compile, want at least 1")
		}
		beforeClose := logger.count()
		if err := engine.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		if logger.count() <= beforeClose {
			t.Fatalf("logger recorded no additional message after Close, want at least 1 more")
		}
	})

	t.Run("WithCacheSizeEvictsAtConfiguredCapacity", func(t *testing.T) {
		engine, err := grpolicy.NewEngine(grpolicy.WithCacheSize(1))
		if err != nil {
			t.Fatalf("NewEngine: %v", err)
		}
		defer engine.Close()

		p1, err := engine.Compile("a == 1")
		if err != nil {
			t.Fatalf("Compile(p1): %v", err)
		}
		if _, err := engine.Compile("b == 2"); err != nil {
			t.Fatalf("Compile(p2): %v", err)
		}
		p1Again, err := engine.Compile("a == 1")
		if err != nil {
			t.Fatalf("Compile(p1 again): %v", err)
		}
		if p1 == p1Again {
			t.Fatalf("with WithCacheSize(1), recompiling p1 after compiling a second distinct policy returned the same cached value — p1 should have been evicted")
		}
	})

	t.Run("WithUnboundedCacheNeverEvicts", func(t *testing.T) {
		engine, err := grpolicy.NewEngine(grpolicy.WithUnboundedCache())
		if err != nil {
			t.Fatalf("NewEngine: %v", err)
		}
		defer engine.Close()

		p1, err := engine.Compile("a == 1")
		if err != nil {
			t.Fatalf("Compile(p1): %v", err)
		}
		for _, e := range []string{"b == 2", "c == 3", "d == 4"} {
			if _, err := engine.Compile(e); err != nil {
				t.Fatalf("Compile(%q): %v", e, err)
			}
		}
		p1Again, err := engine.Compile("a == 1")
		if err != nil {
			t.Fatalf("Compile(p1 again): %v", err)
		}
		if p1 != p1Again {
			t.Fatalf("with WithUnboundedCache, recompiling p1 after several other distinct policies returned a different value — p1 should never be evicted")
		}
	})

	t.Run("WithCacheSizeNonPositiveFallsBackToDefault", func(t *testing.T) {
		// <= 0 must fall back to the documented default, never silently
		// unbounded — proven behaviorally: compiling one policy and
		// confirming the engine still constructs and compiles successfully
		// is the observable half of this contract (the other half, that it
		// is bounded rather than unbounded, is covered by
		// WithCacheSizeEvictsAtConfiguredCapacity above using an explicit
		// positive size).
		engine, err := grpolicy.NewEngine(grpolicy.WithCacheSize(0))
		if err != nil {
			t.Fatalf("NewEngine: %v", err)
		}
		defer engine.Close()
		if _, err := engine.Compile("a == 1"); err != nil {
			t.Fatalf("Compile: %v", err)
		}
	})
}

// testFunctionCallDispatchEdgeCases exercises functionRegistry.call's
// reflect-based dispatch branches that the other scenarios don't reach:
// variadic functions, zero-return functions, (value, error)-returning
// functions on both the success and failure path, a function returning too
// many values (rejected), and numeric-kind auto-conversion between a
// registered function's declared parameter type and the attribute's actual
// runtime type.
func testFunctionCallDispatchEdgeCases(t *testing.T, newEngine func() (grpolicy.Engine, error)) {
	t.Helper()
	engine, err := newEngine()
	if err != nil {
		t.Fatalf("newEngine: %v", err)
	}
	defer engine.Close()
	ctx := context.Background()

	if err := engine.RegisterFunction("sumInts", func(nums ...int) int {
		total := 0
		for _, n := range nums {
			total += n
		}
		return total
	}); err != nil {
		t.Fatalf("RegisterFunction(sum): %v", err)
	}
	if err := engine.RegisterFunction("noop", func() {}); err != nil {
		t.Fatalf("RegisterFunction(noop): %v", err)
	}
	if err := engine.RegisterFunction("mayFail", func(x int) (int, error) {
		if x < 0 {
			return 0, fmt.Errorf("mayFail: negative input %d", x)
		}
		return x * 2, nil
	}); err != nil {
		t.Fatalf("RegisterFunction(mayFail): %v", err)
	}
	if err := engine.RegisterFunction("tooMany", func() (int, int, int) { return 1, 2, 3 }); err != nil {
		t.Fatalf("RegisterFunction(tooMany): %v", err)
	}
	if err := engine.RegisterFunction("convertTest", func(f float64) float64 { return f * 2 }); err != nil {
		t.Fatalf("RegisterFunction(convertTest): %v", err)
	}

	t.Run("Variadic", func(t *testing.T) {
		policy, err := engine.Compile("sumInts(1, 2, 3) == 6")
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		decision, err := policy.Evaluate(ctx, map[string]any{})
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if !decision.Allowed {
			t.Fatalf("Allowed = false, want true")
		}
	})

	t.Run("ZeroReturnValues", func(t *testing.T) {
		policy, err := engine.Compile("noop() == nil")
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		decision, err := policy.Evaluate(ctx, map[string]any{})
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if !decision.Allowed {
			t.Fatalf("Allowed = false, want true (noop() returns nil, nil == nil)")
		}
	})

	t.Run("ValueErrorReturn_success", func(t *testing.T) {
		policy, err := engine.Compile("mayFail(x) == 10")
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		decision, err := policy.Evaluate(ctx, map[string]any{"x": 5})
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if !decision.Allowed {
			t.Fatalf("Allowed = false, want true")
		}
	})

	t.Run("ValueErrorReturn_failure", func(t *testing.T) {
		policy, err := engine.Compile("mayFail(x) == 10")
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		_, err = policy.Evaluate(ctx, map[string]any{"x": -1})
		if err == nil {
			t.Fatalf("Evaluate error = nil, want the error mayFail returned to surface")
		}
	})

	t.Run("TooManyReturnValuesRejected", func(t *testing.T) {
		policy, err := engine.Compile("tooMany() == 1")
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		_, err = policy.Evaluate(ctx, map[string]any{})
		if err == nil {
			t.Fatalf("Evaluate error = nil, want a rejection for a function returning more than (value, error)")
		}
	})

	t.Run("NumericKindAutoConversion", func(t *testing.T) {
		// x is an int attribute; convertTest declares a float64 parameter —
		// int is not directly assignable to float64 but is convertible, and
		// both are numeric kinds, so the call must succeed via conversion.
		policy, err := engine.Compile("convertTest(x) == 10")
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		decision, err := policy.Evaluate(ctx, map[string]any{"x": 5})
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if !decision.Allowed {
			t.Fatalf("Allowed = false, want true (int attribute auto-converted to float64 parameter)")
		}
	})
}

// testExpressionTextIncludesClosingDelimiters is a regression test for a
// real bug caught by running example/example.go directly (not by any test
// — the earlier automated suite never asserted exact Expression text for a
// call or array literal): a CallNode's or ArrayNode's Expression field was
// missing its trailing ")"/"]" entirely (e.g. "isBusinessHours(hour" instead
// of "isBusinessHours(hour)"), because expr-lang/expr's own Location() for
// these node kinds covers only the bare name/opening bracket, and no
// argument's own span reaches the closing delimiter either. See
// docs/architecture.md for the fix (bracket-matching from the opening
// delimiter) and why source-slicing alone was insufficient.
func testExpressionTextIncludesClosingDelimiters(t *testing.T, newEngine func() (grpolicy.Engine, error)) {
	t.Helper()
	engine, err := newEngine()
	if err != nil {
		t.Fatalf("newEngine: %v", err)
	}
	defer engine.Close()
	ctx := context.Background()

	if err := engine.RegisterFunction("double", func(x int) int { return x * 2 }); err != nil {
		t.Fatalf("RegisterFunction: %v", err)
	}

	cases := []struct {
		name         string
		expr         string
		attrs        map[string]any
		wantChildExp string // Expression text expected on Trace[0].Children[0]
	}{
		{"ZeroArgCall", "now() <= now()", map[string]any{}, "now()"},
		{"OneArgCall", "double(x) == 10", map[string]any{"x": 5}, "double(x)"},
		{"NestedCall", "double(double(x)) == 20", map[string]any{"x": 5}, "double(double(x))"},
		{"ArrayLiteral", "x in [1, 2, 3]", map[string]any{"x": 2}, "x"}, // left child is "x"; right child checked separately below
		{"MemberBracketAccess", `resource["ownerID"] == "u1"`, map[string]any{"resource": map[string]any{"ownerID": "u1"}}, `resource["ownerID"]`},
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
			if len(decision.Trace) != 1 || len(decision.Trace[0].Children) == 0 {
				t.Fatalf("Trace shape unexpected for %q: %+v", c.expr, decision.Trace)
			}
			got := decision.Trace[0].Children[0].Expression
			if got != c.wantChildExp {
				t.Fatalf("Trace[0].Children[0].Expression = %q, want %q (closing delimiter must not be dropped)", got, c.wantChildExp)
			}
		})
	}

	t.Run("ArrayLiteralClosingBracket", func(t *testing.T) {
		policy, err := engine.Compile("x in [1, 2, 3]")
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		decision, err := policy.Evaluate(ctx, map[string]any{"x": 2})
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		right := decision.Trace[0].Children[1]
		if right.Expression != "[1, 2, 3]" {
			t.Fatalf("array literal Expression = %q, want %q (closing bracket must not be dropped)", right.Expression, "[1, 2, 3]")
		}
	})

	t.Run("MemberBracketClosingBracket", func(t *testing.T) {
		policy, err := engine.Compile(`resource["ownerID"] == "u1"`)
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		decision, err := policy.Evaluate(ctx, map[string]any{"resource": map[string]any{"ownerID": "u1"}})
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		left := decision.Trace[0].Children[0]
		if left.Expression != `resource["ownerID"]` {
			t.Fatalf(`member bracket-access Expression = %q, want %q (closing bracket must not be dropped)`, left.Expression, `resource["ownerID"]`)
		}
	})
}

// testMemberAccessCollections covers Stage 2 of the member-access feature
// (docs/plan/member-access-support-plan.md, docs/architecture.md §12): dot
// and bracket access against map[string]any, general maps (via reflect),
// and slices/arrays. No struct support yet (Stage 3) and no "?." yet
// (Stage 4) — a nil base always fails loud here.
func testMemberAccessCollections(t *testing.T, newEngine func() (grpolicy.Engine, error)) {
	t.Helper()
	ctx := context.Background()

	newPolicy := func(t *testing.T, expr string) grpolicy.CompiledPolicy {
		t.Helper()
		engine, err := newEngine()
		if err != nil {
			t.Fatalf("newEngine: %v", err)
		}
		t.Cleanup(func() { _ = engine.Close() })
		policy, err := engine.Compile(expr)
		if err != nil {
			t.Fatalf("Compile(%q): %v", expr, err)
		}
		return policy
	}

	t.Run("DotAndBracketAccessProduceSameResult", func(t *testing.T) {
		attrs := map[string]any{"resource": map[string]any{"ownerID": "u1"}}

		dotDecision, err := newPolicy(t, `resource.ownerID == "u1"`).Evaluate(ctx, attrs)
		if err != nil {
			t.Fatalf("Evaluate(dot): %v", err)
		}
		if !dotDecision.Allowed {
			t.Fatalf("dot access Allowed = false, want true")
		}

		bracketDecision, err := newPolicy(t, `resource["ownerID"] == "u1"`).Evaluate(ctx, attrs)
		if err != nil {
			t.Fatalf("Evaluate(bracket): %v", err)
		}
		if !bracketDecision.Allowed {
			t.Fatalf("bracket access Allowed = false, want true")
		}

		dotMember := dotDecision.Trace[0].Children[0]
		bracketMember := bracketDecision.Trace[0].Children[0]
		if len(dotMember.Children) != 2 || len(bracketMember.Children) != 2 {
			t.Fatalf("MemberNode Children = %d, %d, want 2, 2 (base + property, uniform for dot and bracket)", len(dotMember.Children), len(bracketMember.Children))
		}
		if dotMember.Result != "u1" || bracketMember.Result != "u1" {
			t.Fatalf("MemberNode Result = %v, %v, want u1, u1", dotMember.Result, bracketMember.Result)
		}
	})

	t.Run("MissingMapKeyFailsLoud", func(t *testing.T) {
		decision, err := newPolicy(t, "resource.missingField").Evaluate(ctx, map[string]any{"resource": map[string]any{"ownerID": "u1"}})
		if !errors.Is(err, grpolicy.ErrAttributeMissing) {
			t.Fatalf("Evaluate error = %v, want ErrAttributeMissing", err)
		}
		if decision.Allowed {
			t.Fatalf("Allowed = true on error, want false")
		}
		if len(decision.Trace) != 1 {
			t.Fatalf("Trace has %d root nodes, want 1 (partial trace must still be populated)", len(decision.Trace))
		}
	})

	t.Run("NonStringKeyAgainstMapIsTypeMismatch", func(t *testing.T) {
		_, err := newPolicy(t, "resource[0]").Evaluate(ctx, map[string]any{"resource": map[string]any{"ownerID": "u1"}})
		if !errors.Is(err, grpolicy.ErrTypeMismatch) {
			t.Fatalf("Evaluate error = %v, want ErrTypeMismatch", err)
		}
	})

	t.Run("SliceIndexAccess", func(t *testing.T) {
		decision, err := newPolicy(t, `items[1] == "b"`).Evaluate(ctx, map[string]any{"items": []string{"a", "b", "c"}})
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if !decision.Allowed {
			t.Fatalf("Allowed = false, want true")
		}
	})

	t.Run("ArrayIndexAccess", func(t *testing.T) {
		decision, err := newPolicy(t, "items[2] == 3").Evaluate(ctx, map[string]any{"items": [3]int{1, 2, 3}})
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if !decision.Allowed {
			t.Fatalf("Allowed = false, want true")
		}
	})

	t.Run("OutOfRangeIndexFailsLoud", func(t *testing.T) {
		_, err := newPolicy(t, "items[10]").Evaluate(ctx, map[string]any{"items": []string{"a", "b"}})
		if !errors.Is(err, grpolicy.ErrAttributeMissing) {
			t.Fatalf("Evaluate error = %v, want ErrAttributeMissing", err)
		}
	})

	t.Run("NegativeIndexFailsLoud", func(t *testing.T) {
		// Written as items[i] with i = -1 rather than the literal
		// "items[-1]": expr-lang/expr parses a leading "-" as *ast.UnaryNode,
		// which grpolicy's evalUnary doesn't support (only "!"/"not") — that
		// would fail for an unrelated reason before ever reaching
		// resolveMember, so a negative *value* has to arrive via an
		// attribute instead.
		_, err := newPolicy(t, "items[i]").Evaluate(ctx, map[string]any{"items": []string{"a", "b"}, "i": -1})
		if !errors.Is(err, grpolicy.ErrAttributeMissing) {
			t.Fatalf("Evaluate error = %v, want ErrAttributeMissing", err)
		}
	})

	t.Run("NonIntegerIndexAgainstSliceIsTypeMismatch", func(t *testing.T) {
		_, err := newPolicy(t, `items["x"]`).Evaluate(ctx, map[string]any{"items": []string{"a", "b"}})
		if !errors.Is(err, grpolicy.ErrTypeMismatch) {
			t.Fatalf("Evaluate error = %v, want ErrTypeMismatch", err)
		}
	})

	t.Run("NilMapBaseFailsLoud", func(t *testing.T) {
		var nilMap map[string]any
		_, err := newPolicy(t, "resource.ownerID").Evaluate(ctx, map[string]any{"resource": nilMap})
		if !errors.Is(err, grpolicy.ErrAttributeMissing) {
			t.Fatalf("Evaluate error = %v, want ErrAttributeMissing", err)
		}
	})

	t.Run("NilSliceBaseFailsLoud", func(t *testing.T) {
		var nilSlice []string
		_, err := newPolicy(t, "items[0]").Evaluate(ctx, map[string]any{"items": nilSlice})
		if !errors.Is(err, grpolicy.ErrAttributeMissing) {
			t.Fatalf("Evaluate error = %v, want ErrAttributeMissing", err)
		}
	})

	t.Run("StringBaseIndexingUnsupported", func(t *testing.T) {
		_, err := newPolicy(t, "name[0]").Evaluate(ctx, map[string]any{"name": "hello"})
		if !errors.Is(err, grpolicy.ErrTypeMismatch) {
			t.Fatalf("Evaluate error = %v, want ErrTypeMismatch", err)
		}
	})

	t.Run("NestedMemberChainThreeLevelsDeep", func(t *testing.T) {
		decision, err := newPolicy(t, "a.b.c == 5").Evaluate(ctx, map[string]any{
			"a": map[string]any{"b": map[string]any{"c": 5}},
		})
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if !decision.Allowed {
			t.Fatalf("Allowed = false, want true")
		}
	})

	t.Run("GenericMapNonStringKeyType", func(t *testing.T) {
		decision, err := newPolicy(t, `scores[1] == "b"`).Evaluate(ctx, map[string]any{"scores": map[int]string{1: "b", 2: "c"}})
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if !decision.Allowed {
			t.Fatalf("Allowed = false, want true")
		}
	})

	t.Run("GenericMapMissingKeyFailsLoud", func(t *testing.T) {
		_, err := newPolicy(t, "scores[99]").Evaluate(ctx, map[string]any{"scores": map[int]string{1: "b"}})
		if !errors.Is(err, grpolicy.ErrAttributeMissing) {
			t.Fatalf("Evaluate error = %v, want ErrAttributeMissing", err)
		}
	})

	t.Run("GenericMapKeyTypeMismatch", func(t *testing.T) {
		_, err := newPolicy(t, `scores["x"]`).Evaluate(ctx, map[string]any{"scores": map[int]string{1: "b"}})
		if !errors.Is(err, grpolicy.ErrTypeMismatch) {
			t.Fatalf("Evaluate error = %v, want ErrTypeMismatch", err)
		}
	})

	t.Run("GenericMapConvertibleNumericKey", func(t *testing.T) {
		// propVal ("1") arrives as a plain Go int (IntegerNode's literal
		// type) but the map's key type is int64 — not directly assignable,
		// but numerically convertible, exercising resolveMapMember's
		// Convert path (distinct from GenericMapNonStringKeyType above,
		// where the key type is already a plain int and needs no convert).
		decision, err := newPolicy(t, `scores[1] == "y"`).Evaluate(ctx, map[string]any{"scores": map[int64]string{1: "y"}})
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if !decision.Allowed {
			t.Fatalf("Allowed = false, want true")
		}
	})

	t.Run("GenericMapNilKeyIsTypeMismatch", func(t *testing.T) {
		_, err := newPolicy(t, "scores[k]").Evaluate(ctx, map[string]any{"scores": map[int]string{1: "b"}, "k": nil})
		if !errors.Is(err, grpolicy.ErrTypeMismatch) {
			t.Fatalf("Evaluate error = %v, want ErrTypeMismatch", err)
		}
	})

	t.Run("MissingBaseIdentifierFailsLoud", func(t *testing.T) {
		// "resource" itself isn't in attrs at all — the error must
		// propagate up from evalIdentifier through evalMember unchanged,
		// distinct from every other test above where "resource"/"items" is
		// present but nil, missing a key, or out of range.
		_, err := newPolicy(t, "resource.ownerID").Evaluate(ctx, map[string]any{})
		if !errors.Is(err, grpolicy.ErrAttributeMissing) {
			t.Fatalf("Evaluate error = %v, want ErrAttributeMissing", err)
		}
	})

	t.Run("MissingPropertyIdentifierFailsLoud", func(t *testing.T) {
		// Bracket-form property is itself an expression (an identifier
		// here) that can fail independently of the base.
		_, err := newPolicy(t, "items[i]").Evaluate(ctx, map[string]any{"items": []string{"a", "b"}})
		if !errors.Is(err, grpolicy.ErrAttributeMissing) {
			t.Fatalf("Evaluate error = %v, want ErrAttributeMissing", err)
		}
	})

	t.Run("UntypedNilBaseFailsLoud", func(t *testing.T) {
		// A literal untyped nil stored in attrs, distinct from
		// NilMapBaseFailsLoud/NilSliceBaseFailsLoud above (a typed nil
		// map/slice, which is never == nil as a bare interface comparison).
		_, err := newPolicy(t, "resource.ownerID").Evaluate(ctx, map[string]any{"resource": nil})
		if !errors.Is(err, grpolicy.ErrAttributeMissing) {
			t.Fatalf("Evaluate error = %v, want ErrAttributeMissing", err)
		}
	})

	t.Run("IndexKindCoercionMatrix", func(t *testing.T) {
		// Every integer kind an attribute value might actually carry (no
		// float truncation — that's NonIntegerIndexAgainstSliceIsTypeMismatch
		// above), mirroring testTypeCoercionMatrix's own numeric-kind
		// coverage style for compareValues/toFloat.
		cases := []struct {
			name string
			idx  any
		}{
			{"Int8", int8(1)},
			{"Int16", int16(1)},
			{"Int32", int32(1)},
			{"Int64", int64(1)},
			{"Uint", uint(1)},
			{"Uint8", uint8(1)},
			{"Uint16", uint16(1)},
			{"Uint32", uint32(1)},
			{"Uint64", uint64(1)},
		}
		policy := newPolicy(t, `items[i] == "b"`)
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				decision, err := policy.Evaluate(ctx, map[string]any{"items": []string{"a", "b", "c"}, "i": c.idx})
				if err != nil {
					t.Fatalf("Evaluate: %v", err)
				}
				if !decision.Allowed {
					t.Fatalf("Allowed = false, want true for index type %T", c.idx)
				}
			})
		}
	})

	t.Run("IndexOverflowGuardFailsLoud", func(t *testing.T) {
		// uint/uint64 are the only kinds toInt guards against overflowing
		// int on the 64-bit platforms grpolicy targets (see toInt's doc
		// comment) — a value this large must be a type mismatch, never
		// silently wrapped to a negative/bogus index.
		cases := []struct {
			name string
			idx  any
		}{
			{"UintTooLarge", uint(math.MaxUint64)},
			{"Uint64TooLarge", uint64(math.MaxUint64)},
		}
		policy := newPolicy(t, "items[i]")
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				_, err := policy.Evaluate(ctx, map[string]any{"items": []string{"a", "b"}, "i": c.idx})
				if !errors.Is(err, grpolicy.ErrTypeMismatch) {
					t.Fatalf("Evaluate error = %v, want ErrTypeMismatch", err)
				}
			})
		}
	})
}

// memberAccessInner/memberAccessResource support testMemberAccessStructFields
// (Stage 3 of the member-access feature): an embedded type to prove
// FieldByName resolves promoted fields, an exported field, and an
// unexported field to prove it's never exposed even by probing.
type memberAccessInner struct {
	City string
}

type memberAccessResource struct {
	memberAccessInner
	OwnerID string
	secret  string // deliberately unexported — accessed only via reflect in the test itself
}

// testMemberAccessStructFields covers Stage 3 of the member-access feature
// (docs/plan/member-access-support-plan.md, docs/architecture.md §12):
// struct field access via reflect, exported fields only, both dot and
// bracket syntax, pointer-to-struct auto-dereferenced, embedded/promoted
// fields, and the resolveMember panic-recovery backstop.
func testMemberAccessStructFields(t *testing.T, newEngine func() (grpolicy.Engine, error)) {
	t.Helper()
	ctx := context.Background()

	newPolicy := func(t *testing.T, expr string) grpolicy.CompiledPolicy {
		t.Helper()
		engine, err := newEngine()
		if err != nil {
			t.Fatalf("newEngine: %v", err)
		}
		t.Cleanup(func() { _ = engine.Close() })
		policy, err := engine.Compile(expr)
		if err != nil {
			t.Fatalf("Compile(%q): %v", expr, err)
		}
		return policy
	}

	t.Run("ExportedFieldDotAndBracketAccess", func(t *testing.T) {
		resource := memberAccessResource{OwnerID: "u1"}

		dotDecision, err := newPolicy(t, `resource.OwnerID == "u1"`).Evaluate(ctx, map[string]any{"resource": resource})
		if err != nil {
			t.Fatalf("Evaluate(dot): %v", err)
		}
		if !dotDecision.Allowed {
			t.Fatalf("dot field access Allowed = false, want true")
		}

		bracketDecision, err := newPolicy(t, `resource["OwnerID"] == "u1"`).Evaluate(ctx, map[string]any{"resource": resource})
		if err != nil {
			t.Fatalf("Evaluate(bracket): %v", err)
		}
		if !bracketDecision.Allowed {
			t.Fatalf("bracket field access Allowed = false, want true")
		}
	})

	t.Run("UnexportedFieldFailsLoudNotPanics", func(t *testing.T) {
		// Reaching this assertion at all (rather than crashing the test
		// binary) is itself part of the proof, same framing as this repo's
		// existing CustomFunctionPanicIsRecoveredNotCrashed.
		resource := memberAccessResource{OwnerID: "u1", secret: "s"}
		_, err := newPolicy(t, "resource.secret").Evaluate(ctx, map[string]any{"resource": resource})
		if !errors.Is(err, grpolicy.ErrAttributeMissing) {
			t.Fatalf("Evaluate error = %v, want ErrAttributeMissing", err)
		}
	})

	t.Run("UnknownFieldNameFailsLoud", func(t *testing.T) {
		resource := memberAccessResource{OwnerID: "u1"}
		_, err := newPolicy(t, "resource.NoSuchField").Evaluate(ctx, map[string]any{"resource": resource})
		if !errors.Is(err, grpolicy.ErrAttributeMissing) {
			t.Fatalf("Evaluate error = %v, want ErrAttributeMissing", err)
		}
	})

	t.Run("PointerToStructDereferencedAutomatically", func(t *testing.T) {
		resource := &memberAccessResource{OwnerID: "u1"}
		decision, err := newPolicy(t, `resource.OwnerID == "u1"`).Evaluate(ctx, map[string]any{"resource": resource})
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if !decision.Allowed {
			t.Fatalf("Allowed = false, want true")
		}
	})

	t.Run("NilPointerBaseFailsLoudNotPanics", func(t *testing.T) {
		var resource *memberAccessResource
		_, err := newPolicy(t, "resource.OwnerID").Evaluate(ctx, map[string]any{"resource": resource})
		if !errors.Is(err, grpolicy.ErrAttributeMissing) {
			t.Fatalf("Evaluate error = %v, want ErrAttributeMissing", err)
		}
	})

	t.Run("NilInnerPointerAfterOuterDereferenceFailsLoud", func(t *testing.T) {
		// A non-nil **memberAccessResource pointing to a nil
		// *memberAccessResource: isNilBase (checked before resolveMember is
		// ever called) only sees the outer pointer, which isn't nil — the
		// nil is only discovered one level in, exercising resolveMember's
		// own pointer-deref loop's nil check, distinct from
		// NilPointerBaseFailsLoudNotPanics above (a nil pointer at the
		// outermost level, caught by isNilBase itself).
		var inner *memberAccessResource
		outer := &inner
		_, err := newPolicy(t, "resource.OwnerID").Evaluate(ctx, map[string]any{"resource": outer})
		if !errors.Is(err, grpolicy.ErrAttributeMissing) {
			t.Fatalf("Evaluate error = %v, want ErrAttributeMissing", err)
		}
	})

	t.Run("EmbeddedPromotedFieldReachable", func(t *testing.T) {
		resource := memberAccessResource{memberAccessInner: memberAccessInner{City: "NYC"}}
		decision, err := newPolicy(t, `resource.City == "NYC"`).Evaluate(ctx, map[string]any{"resource": resource})
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if !decision.Allowed {
			t.Fatalf("Allowed = false, want true")
		}
	})

	t.Run("NonStringPropertyAgainstStructIsTypeMismatch", func(t *testing.T) {
		resource := memberAccessResource{OwnerID: "u1"}
		_, err := newPolicy(t, "resource[0]").Evaluate(ctx, map[string]any{"resource": resource})
		if !errors.Is(err, grpolicy.ErrTypeMismatch) {
			t.Fatalf("Evaluate error = %v, want ErrTypeMismatch", err)
		}
	})

	t.Run("UnhashableGenericMapKeyPanicRecovered", func(t *testing.T) {
		// A real, reachable-via-public-API panic source: reflect.Value.
		// MapIndex panics ("hash of unhashable type") when the map's key
		// type is an interface (any concrete type is AssignableTo it, so
		// resolveMapMember's own pre-checks never reject it) and the actual
		// key value's dynamic type isn't hashable (a slice here) — confirmed
		// empirically, not assumed, before writing this test. This proves
		// resolveMember's recover backstop (added alongside struct support
		// in this stage) actually recovers, rather than crashing the
		// process — reaching the assertion at all is part of the proof.
		_, err := newPolicy(t, "m[k]").Evaluate(ctx, map[string]any{
			"m": map[any]string{"x": "y"},
			"k": []int{1, 2, 3},
		})
		if err == nil {
			t.Fatal("Evaluate error = nil, want a non-nil error (panic must be recovered, not crash the process)")
		}
	})
}

// testOptionalChainShortCircuit covers Stage 4 of the member-access feature
// (docs/plan/member-access-support-plan.md, docs/architecture.md §12):
// *ast.ChainNode / MemberNode.Optional short-circuit semantics, mirroring
// testShortCircuitAndTrace's own falsifying-test discipline (a call
// counter, not just a returned value, proves Property is genuinely never
// evaluated on the short-circuit path).
//
// The asymmetric rule this proves (verified empirically against real
// expr-lang/expr — see docs/architecture.md §12, Decision B): once the
// first "?." is hit walking outward from the base, every subsequent DOT
// link tunnels a nil through to the end, regardless of that link's own
// Optional flag; a link written *before* the first "?." is never
// protected; and — the non-obvious part — a BRACKET-index link
// immediately following a closed chain (i.e. "a?.b[0]") is *not*
// protected either, because expr-lang/expr's own parser doesn't fold a
// bracket continuation into the same ChainNode the way it does a dot
// continuation (confirmed against real expr-lang/expr's own VM, which
// raises its own runtime error for exactly this shape, not a graceful
// nil) — only a bracket step that is *itself* optional ("?.[") is
// protected.
func testOptionalChainShortCircuit(t *testing.T, newEngine func() (grpolicy.Engine, error)) {
	t.Helper()
	ctx := context.Background()

	newPolicy := func(t *testing.T, engine grpolicy.Engine, expr string) grpolicy.CompiledPolicy {
		t.Helper()
		policy, err := engine.Compile(expr)
		if err != nil {
			t.Fatalf("Compile(%q): %v", expr, err)
		}
		return policy
	}

	t.Run("NilBaseShortCircuitsSingleLink", func(t *testing.T) {
		engine, err := newEngine()
		if err != nil {
			t.Fatalf("newEngine: %v", err)
		}
		defer engine.Close()

		decision, err := newPolicy(t, engine, "a?.b").Evaluate(ctx, map[string]any{"a": nil})
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if decision.Allowed {
			t.Fatalf("Allowed = true, want false (a tunneled nil is never truthy)")
		}
		if len(decision.Trace) != 1 {
			t.Fatalf("Trace has %d root nodes, want 1", len(decision.Trace))
		}
		root := decision.Trace[0]
		if !root.Evaluated {
			t.Fatalf("root.Evaluated = false, want true (a tunneled-to-nil link has a definite, legitimate value: nil)")
		}
		if root.Result != nil {
			t.Fatalf("root.Result = %#v, want nil", root.Result)
		}
		if len(root.Children) != 2 || root.Children[1].Evaluated {
			t.Fatalf("root.Children[1] (Property) Evaluated = %v, want false — Property must never be evaluated on the short-circuit path", root.Children[1].Evaluated)
		}
	})

	t.Run("NilIntermediateShortCircuitsRemainingLinksWithoutSideEffects", func(t *testing.T) {
		// a?.b?.[expensiveIndex()]: the bracket step is itself optional
		// ("?.["), so — unlike the unprotected a?.b[expensiveIndex()]
		// shape covered by BracketContinuationAfterOptionalChainIsNotProtected
		// below — this one *does* tunnel, and expensiveIndex() must never
		// be called (confirmed empirically against real expr-lang/expr
		// before writing this test: it returns nil with the call count at
		// zero for exactly this shape).
		engine, err := newEngine()
		if err != nil {
			t.Fatalf("newEngine: %v", err)
		}
		defer engine.Close()

		var calls int32
		if err := engine.RegisterFunction("expensiveIndex", func() int {
			atomic.AddInt32(&calls, 1)
			return 0
		}); err != nil {
			t.Fatalf("RegisterFunction: %v", err)
		}

		decision, err := newPolicy(t, engine, "a?.b?.[expensiveIndex()]").Evaluate(ctx, map[string]any{"a": nil})
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if got := atomic.LoadInt32(&calls); got != 0 {
			t.Fatalf("expensiveIndex() was called %d times, want 0 — nil tunnel did not actually prevent Property evaluation", got)
		}
		if decision.Allowed {
			t.Fatalf("Allowed = true, want false")
		}
	})

	t.Run("LinkBeforeFirstOptionalIsNotProtected", func(t *testing.T) {
		engine, err := newEngine()
		if err != nil {
			t.Fatalf("newEngine: %v", err)
		}
		defer engine.Close()

		_, err = newPolicy(t, engine, "a.b?.c").Evaluate(ctx, map[string]any{"a": nil})
		if !errors.Is(err, grpolicy.ErrAttributeMissing) {
			t.Fatalf("Evaluate error = %v, want ErrAttributeMissing (a.b is written before the first '?.', never protected)", err)
		}
	})

	t.Run("PlainDotAfterFirstOptionalStillTunnels", func(t *testing.T) {
		engine, err := newEngine()
		if err != nil {
			t.Fatalf("newEngine: %v", err)
		}
		defer engine.Close()

		decision, err := newPolicy(t, engine, "a?.b.c.d").Evaluate(ctx, map[string]any{"a": nil})
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if decision.Allowed {
			t.Fatalf("Allowed = true, want false")
		}
	})

	t.Run("BracketContinuationAfterOptionalChainIsNotProtected", func(t *testing.T) {
		engine, err := newEngine()
		if err != nil {
			t.Fatalf("newEngine: %v", err)
		}
		defer engine.Close()

		_, err = newPolicy(t, engine, "a?.b[0]").Evaluate(ctx, map[string]any{"a": nil})
		if !errors.Is(err, grpolicy.ErrAttributeMissing) {
			t.Fatalf("Evaluate error = %v, want ErrAttributeMissing (a bracket-index continuation right after a closed chain is not protected — see docs/architecture.md §12)", err)
		}
	})

	t.Run("MissingKeyUnderOptionalStillFailsLoud", func(t *testing.T) {
		// Decision A: "?." guards against nil, never against absence.
		engine, err := newEngine()
		if err != nil {
			t.Fatalf("newEngine: %v", err)
		}
		defer engine.Close()

		_, err = newPolicy(t, engine, "resource?.missingKey").Evaluate(ctx, map[string]any{"resource": map[string]any{"present": true}})
		if !errors.Is(err, grpolicy.ErrAttributeMissing) {
			t.Fatalf("Evaluate error = %v, want ErrAttributeMissing", err)
		}
	})

	t.Run("MissingBaseIdentifierStillFailsLoudEvenUnderOptional", func(t *testing.T) {
		// "a" isn't in attrs at all — absence of the base identifier itself
		// is a hard error regardless of "?." anywhere in the expression,
		// distinct from NilBaseShortCircuitsSingleLink above (where "a" is
		// present but its value is nil).
		engine, err := newEngine()
		if err != nil {
			t.Fatalf("newEngine: %v", err)
		}
		defer engine.Close()

		_, err = newPolicy(t, engine, "a?.b").Evaluate(ctx, map[string]any{})
		if !errors.Is(err, grpolicy.ErrAttributeMissing) {
			t.Fatalf("Evaluate error = %v, want ErrAttributeMissing", err)
		}
	})

	t.Run("LegitimatelyNilValueTunnelsSameAsShortCircuit", func(t *testing.T) {
		// "owner" genuinely exists as a key and genuinely stores nil (not a
		// missing key, not a "resource" that's itself nil) — tunnels
		// identically to any other nil, indistinguishable by design (see
		// docs/architecture.md §12).
		engine, err := newEngine()
		if err != nil {
			t.Fatalf("newEngine: %v", err)
		}
		defer engine.Close()

		decision, err := newPolicy(t, engine, "resource.owner?.name").Evaluate(ctx, map[string]any{
			"resource": map[string]any{"owner": nil},
		})
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if decision.Allowed {
			t.Fatalf("Allowed = true, want false")
		}
	})
}

// TestEngine_Contract runs the full behavioral contract suite above
// against grpolicy.NewEngine.
func TestEngine_Contract(t *testing.T) {
	runEngineContract(t, func() (grpolicy.Engine, error) {
		return grpolicy.NewEngine()
	})
}
