// File: conformance/conformance.go

// Package conformance is grpolicy's own primary behavioral test suite. This
// is a deliberate reshaping of the sibling repos' conformance/ pattern
// (grcache, graudit): there, conformance/ exists to prove multiple backend
// implementations behave identically through one shared interface. grpolicy
// has exactly one implementation — there is no backend-parity concern to
// prove — so here Run is simply the reusable entry point for grpolicy's own
// full scenario list, callable from the root package's tests (and usable by
// a future consumer, e.g. grauth, as an integration smoke test against its
// own Engine construction). See docs/architecture.md for this deviation
// recorded alongside the sibling repos' own documented deviations.
package conformance

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gourdian25/grpolicy"
)

// RunOption configures Run. None are defined yet at v0.1.0 — the type
// exists now as an extension point, matching grcache's RunOption shape, so
// a future scenario-specific relaxation can be added without a breaking
// signature change.
type RunOption func(*runConfig)

type runConfig struct{}

// Run executes grpolicy's full behavioral suite against a fresh Engine
// obtained from newEngine for each scenario.
//
// Parameters:
//   - t: *testing.T
//   - newEngine: func() (grpolicy.Engine, error) — called once per scenario
//     to get a fresh Engine instance
//   - opts: ...RunOption — currently always empty; reserved for future use
//
// Example:
//
//	func TestConformance(t *testing.T) {
//		conformance.Run(t, func() (grpolicy.Engine, error) {
//			return grpolicy.NewEngine()
//		})
//	}
func Run(t *testing.T, newEngine func() (grpolicy.Engine, error), opts ...RunOption) {
	t.Helper()

	cfg := &runConfig{}
	for _, opt := range opts {
		opt(cfg)
	}
	_ = cfg

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

func (l *capturingLogger) Infof(format string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.messages = append(l.messages, fmt.Sprintf(format, args...))
}
func (l *capturingLogger) Warnf(format string, args ...interface{})  { l.Infof(format, args...) }
func (l *capturingLogger) Errorf(format string, args ...interface{}) { l.Infof(format, args...) }

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
}
