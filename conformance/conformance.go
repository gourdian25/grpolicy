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
					errCh <- fmt.Errorf("Evaluate after Compile(%q): %w", expr, err)
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
