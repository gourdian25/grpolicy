# grpolicy — v0.1.0 Scope & Implementation Plan

**Repo path (to be created):** `~/Dev/gourdian25/grpolicy`
**Reference repos already built (explore ALL before designing anything):**
`~/Dev/gourdian25/gourdiantoken`, `~/Dev/gourdian25/grlog`,
`~/Dev/gourdian25/grcache`, `~/Dev/gourdian25/grevents`,
`~/Dev/gourdian25/graudit`

**Position in build order:** 6th repo. `grauth` follows this one directly and
is grpolicy's primary consumer — every RBAC/ABAC decision `grauth` makes will
route through `grpolicy.Evaluate()`. Design the API now with that consumer in
mind, even though `grauth` doesn't exist yet.

---

## 0. Mandatory pre-design research — explore the whole family first

Before any architecture decision below is finalized, read all five existing
repos in full. This one matters more than usual, because grpolicy is
architecturally the most different repo in the ecosystem so far (no storage
backend, no network I/O, a genuine language-design problem at its core) —
which makes it tempting to treat it as a clean slate. It isn't. Confirm and
carry forward:

1. **From `grcache`**: the subpackage-per-backend reasoning (does grpolicy
   need any equivalent split — e.g. separate packages per built-in function
   set — or is a single flat package correct here, unlike grcache?), the
   `sync.Once`-guarded `Close()` idempotency pattern, the sentinel-error
   style (`errors.Is`-compatible, no `IsX(err) bool` helpers), the optional
   structural `Logger` interface, and the `conformance/` shared-test-suite
   pattern. Also re-read the `Pipeline`/`TxPipeline` CHANGELOG entry — the
   lesson (don't document a guarantee the code doesn't enforce) applies
   directly to grpolicy's explainability claims: if `Decision.Trace` is
   documented as showing "exactly how the result was reached," verify that's
   literally true for every operator, not just the common cases.
2. **From `grevents`**: the goroutine-fan-out and overflow-strategy audit
   findings — grpolicy has no async delivery, but the general discipline of
   "write a test that would actually falsify a concurrency claim, don't just
   read the code" applies to grpolicy's compiled-policy cache (§7), which
   will be accessed concurrently by many `Evaluate()` calls.
3. **From `graudit`**: the hash-chain serialization lesson (a correctness
   property that looks fine in isolated testing and breaks under real
   concurrent load) and the "precise, non-aspirational claim" documentation
   pattern (graudit's README explicitly states what hash-chaining does and
   does NOT prove). grpolicy's `Decision.Trace` needs the same treatment —
   state precisely what the trace does and doesn't capture (e.g. does it
   show short-circuited branches of `&&`/`||` that were never evaluated, or
   only branches that actually ran? Both are defensible; pick one and say so
   explicitly rather than leaving it ambiguous).
4. **From `gourdiantoken`**: sentinel error naming/wrapping conventions,
   for consistency across all six now-existing repos.
5. **From `grlog`**: confirm the structural `Logger` interface shape
   (`Infof`/`Warnf`/`Errorf`) is identical to what every other sibling
   repo has adopted, and reuse it verbatim.

Only after this research should the core architecture decision in §2 be
finalized.

---

## 1. Vision

grpolicy is a standalone policy/expression evaluation engine. It is **not**
authentication and **not** authorization — it knows nothing about "users,"
"roles," or "permissions." Given a compiled expression and a map of named
attributes, it returns a boolean decision plus an explainable trace of
exactly how that decision was reached. `grauth` will own the vocabulary of
"who has which role"; grpolicy only ever sees attribute maps.

---

## 2. The core architecture decision: wrap an existing library, don't hand-roll a parser

This is the single most important decision in this repo, and it should be
made deliberately, not by default momentum toward "let's write our own."

### 2.1 The real cost of hand-rolling

Writing a correct, safe expression parser from scratch is a genuine
language-design project: tokenizer, recursive-descent (or Pratt) parser,
operator-precedence handling, a full test suite for parser edge cases and
malformed input, and — critically for something that will gate real access
decisions in `grauth` — a security review of the parser itself (a
policy-evaluation engine with a parser bug that causes a `DENY`-should-be
expression to evaluate as `ALLOW` is a security vulnerability, not just a
correctness bug). This is realistically a multi-month effort done properly,
and it's the kind of code where subtle bugs are exceptionally dangerous
given what depends on it.

### 2.2 Recommendation: use `expr-lang/expr` for parsing, write grpolicy's own tree-walking evaluator

- **Use `expr-lang/expr` (or evaluate `google/cel-go` as the alternative —
  see §2.3) only for lexing/parsing source text into an AST.** Do not use
  its own `Run`/`Program` execution path as grpolicy's evaluator.
- **Write grpolicy's own evaluator that walks the parsed AST directly.**
  This is the part that actually produces `Decision.Trace` — building
  explainability into a tree-walking evaluator you control is
  straightforward (attach a `CheckNode` at each AST node as you evaluate
  it); bolting the same explainability onto a third-party library's
  compiled bytecode VM (which is what `expr-lang/expr`'s `Run` actually
  executes) is not, and trying to do so risks a second, subtly different
  reimplementation of evaluation semantics that could diverge from what the
  library's own `Run` would produce — which would be a genuine correctness
  bug (grpolicy's explain-trace showing a different result than what
  actually gated the decision).
- **This means grpolicy reuses the hardest, highest-risk 80% of a parser
  (tokenizing, grammar, precedence — all extremely well-tested in
  `expr-lang/expr`'s real-world usage) while keeping 100% control over the
  actual 20% that is grpolicy's real value-add: evaluation semantics +
  explainability + caching.**

### 2.3 `expr-lang/expr` vs `google/cel-go` — agent to decide, with reasoning shown

Both are legitimate options; the agent should make a real comparison rather
than picking arbitrarily:

- `expr-lang/expr`: Go-idiomatic syntax (closer to Go expressions), simpler
  to embed, MIT-licensed, exposes an `ast.Node` tree via its `Parse`
  function independent of compilation/execution.
- `google/cel-go`: Google's Common Expression Language, more mature/battle
  tested at scale (used in Kubernetes admission control, among others),
  Apache 2.0 licensed, has its own type system that's more rigorous but adds
  conceptual overhead, and its own distinct syntax that's less
  "Go-familiar."

**Agent decision required**: pick one, and justify the choice specifically
against grpolicy's actual requirement (AST access for building a custom
tree-walking evaluator, not just "which one is more popular"). Document the
runner-up's rejection reason in `docs/architecture.md` so a future
contributor doesn't re-litigate this without cause.

### 2.4 If the agent's research concludes hand-rolling is actually justified

Only proceed with a hand-rolled parser if both existing libraries are found
to be fundamentally unable to provide AST access suitable for a custom
evaluator (unlikely, but the agent should verify rather than assume). If
hand-rolling is genuinely chosen, flag this explicitly as a significant
scope/timeline increase versus the recommendation above, and get it
confirmed before proceeding.

---

## 3. Scope

### 3.1 v0.1.0 — in

- **Expression language** covering: boolean logic (`&&`, `||`, `!`),
  comparisons (`==`, `!=`, `<`, `<=`, `>`, `>=`), string operators
  (`contains`, `startsWith`, `endsWith`, regex `matches`), membership
  (`in`), and a small set of built-in functions (`now()`, date/duration
  helpers) — via whichever underlying library is chosen in §2.
- **Custom function registration**: callers can register their own named
  functions (e.g. a future `grauth` might register `isBusinessHours()`)
  before compiling policies that use them.
- **`Compile(expr string) (CompiledPolicy, error)`** — parses and validates
  an expression once; returns a reusable, concurrency-safe compiled form.
- **`Evaluate(ctx, attrs map[string]any) (Decision, error)`** — runs a
  compiled policy against a given attribute map.
- **`Decision.Trace`**: a tree of `CheckNode`s showing which sub-expressions
  were evaluated, their intermediate values, and whether they passed. State
  explicitly (per §0.3) whether short-circuited branches appear in the trace
  or not.
- **Compiled-policy caching**: `Compile()` results are cached by source
  string so repeated compilation of the same policy text is cheap (relevant
  once `grauth` is calling this on every request).
- **Expression complexity limits**: a configurable max AST node count /
  nesting depth, rejected at `Compile()` time — see §6 (security).
- **Evaluation timeout via `ctx`**: `Evaluate()` must respect context
  cancellation/deadline, particularly relevant if a registered custom
  function could be slow (e.g. a regex against attacker-influenced input).

### 3.2 v0.1.0 — explicitly out

- **Any RBAC/ABAC data modeling.** grpolicy has no concept of "role" or
  "permission" as first-class types — only attribute maps. This boundary
  must not blur; if implementation pressure from a future `grauth` design
  starts pulling role/permission vocabulary into grpolicy, that's a sign
  the boundary is wrong, not a reason to add it here.
- **Policy storage.** grpolicy never persists policy text anywhere — where
  the expression string comes from (a config file, a DB row `grauth`
  manages, a hardcoded string) is entirely the caller's problem.
- **Wildcard/hierarchical attribute paths** (e.g. `user.address.city`) — v1
  attributes are flat `map[string]any`; nested-path access is a v0.2+
  candidate if real usage in `grauth` demands it.
- **A policy authoring UI or CLI validator tool** — out of scope for the
  library itself.

---

## 4. Public API

```go
package grpolicy

import (
	"context"
	"time"
)

type Engine interface {
	// Compile parses and validates expr, returning a reusable compiled
	// form. Results are cached internally by source string — calling
	// Compile with identical expr text repeatedly is cheap.
	Compile(expr string) (CompiledPolicy, error)

	// RegisterFunction adds a custom function usable by policies compiled
	// after this call. Must be called before Compile for any policy that
	// references the function name.
	RegisterFunction(name string, fn any) error

	Close() error
}

type CompiledPolicy interface {
	// Evaluate runs the compiled policy against attrs. Respects ctx
	// cancellation/deadline — a slow custom function or pathological input
	// should not be able to hang an Evaluate call indefinitely.
	Evaluate(ctx context.Context, attrs map[string]any) (Decision, error)

	// Source returns the original expression text this was compiled from.
	Source() string
}

type Decision struct {
	Allowed bool
	Trace   []CheckNode
}

type CheckNode struct {
	Expression string      // the sub-expression's source text
	Operator   string      // e.g. "&&", "==", "in", or "" for a leaf
	Result     any         // the value this sub-expression evaluated to
	Passed     bool        // Result coerced to boolean truthiness
	Children   []CheckNode
	Evaluated  bool        // false if short-circuited — see §0.3 for the policy on this
}
```

### 4.1 Sentinel errors

```go
var (
	ErrCompileFailed     = errors.New("grpolicy: expression failed to compile")
	ErrTooComplex        = errors.New("grpolicy: expression exceeds complexity limit")
	ErrUnknownFunction   = errors.New("grpolicy: reference to unregistered function")
	ErrEvaluationTimeout = errors.New("grpolicy: evaluation exceeded context deadline")
	ErrTypeMismatch      = errors.New("grpolicy: attribute type incompatible with operator")
)
```
*(Agent: confirm wrapping style against gourdiantoken/grcache/graudit
convention — likely `fmt.Errorf("grpolicy: compiling %q: %w", expr, ErrCompileFailed)`
or similar; match exactly.)*

---

## 5. Type system and attribute semantics

Attributes are `map[string]any`. Define explicit, documented coercion rules
— this is a common source of subtle bugs in policy engines generally
(mismatched numeric types, string-vs-number comparisons, nil handling):

- Numeric comparisons: normalize `int`, `int64`, `float64` to a common
  numeric type before comparing; document exactly what happens when a
  policy compares a numeric attribute against a string value (should this
  be `ErrTypeMismatch` at evaluation time, or coerced?).
- String comparisons: exact by default; `matches` uses regex.
- Date/time: attributes may be `time.Time` values; comparison operators
  and functions like `now()` must interoperate cleanly.
- `nil`/missing attributes: an expression referencing an attribute key not
  present in the map — is this `ErrTypeMismatch`, a typed "undefined" value
  that propagates predictably through operators, or something else? Pick
  one behavior and document it precisely; this is exactly the kind of thing
  that needs to be a deliberate decision, not whatever the underlying
  library happens to do by default.
- Collections: `in` operator against `[]string`/`[]any` attribute values.

---

## 6. Security considerations

Given policies may eventually be authored by less-trusted actors (e.g. a
future multi-tenant `grauth` where tenant admins define their own ABAC
rules), grpolicy must guard against:

- **ReDoS (regex denial-of-service)** via the `matches` operator — either
  use a regex engine with linear-time guarantees (Go's `regexp` package, RE2
  based, is already ReDoS-safe by construction — confirm this is what's
  actually used, not a different regex library pulled in transitively) or
  document the risk clearly if a different engine is ever substituted.
- **Expression complexity limits** (§3.1) enforced at `Compile()` time —
  reject pathologically large/deep expressions before they ever reach
  `Evaluate()`.
- **Evaluation timeouts via `ctx`** — a custom function registered by a
  consumer could be slow or unbounded; grpolicy's evaluator must check
  `ctx.Done()` between sub-expression evaluations, not just once at the
  start.
- **No code execution beyond registered functions** — confirm the chosen
  underlying library (§2.3) has no mechanism for a policy string to invoke
  arbitrary Go code beyond what's explicitly registered via
  `RegisterFunction`.

---

## 7. Compiled-policy cache

`Compile()` caches by source string. This is an in-process cache (not
`grcache`-backed — a `CompiledPolicy` holds a parsed AST and closures, which
aren't the `[]byte` values `grcache`'s interface expects, so forcing this
through `grcache` would need an awkward serialization layer for no real
benefit). A simple size-bounded cache (LRU or similar) with its own
concurrency-safety is appropriate here — but this must be verified under
real concurrent `Compile()`/`Evaluate()` calls from many goroutines
simultaneously (per §0.2's lesson from the grevents audit), not just
assumed safe because it uses a `sync.Map` or a mutex that looks correct on
read.

**Agent evaluation question**: is a bounded LRU cache with an eviction
policy the right default, or should v0.1.0 ship an unbounded cache (simpler,
but a memory-growth risk if `grauth` ever compiles many distinct
dynamically-generated policy strings)? Recommend bounded with a sane default
size, configurable.

---

## 8. Folder structure

```
grpolicy/
├── engine.go           // Engine interface, NewEngine constructor
├── policy.go             // CompiledPolicy interface, Decision, CheckNode
├── errors.go
├── functions.go            // built-in function registry + RegisterFunction
├── eval.go                   // the custom tree-walking evaluator (the real value-add)
├── cache.go                     // compiled-policy LRU cache
├── logger.go                       // optional structural Logger interface
├── docs.go
└── conformance/
    └── conformance.go                 // shared behavioral suite: every operator,
                                        // short-circuit trace behavior, complexity
                                        // limits, timeout handling, cache correctness
                                        // under concurrent load, custom function
                                        // registration and invocation
```

Single flat package (unlike grcache/graudit's per-backend subpackages) —
there's no storage backend concept here to warrant splitting, per §0.1's
question.

---

## 9. Testing strategy

- **Per-operator conformance tests**: every supported operator, both
  passing and failing cases, with trace assertions (not just the boolean
  result — confirm `CheckNode`s are structured as documented).
- **Short-circuit trace behavior test**: whatever was decided in §0.3, write
  an explicit test proving it (e.g. `false && expensiveFunction()` — did
  `expensiveFunction` actually get called or not, and does the trace
  reflect that correctly via `Evaluated: false`?).
- **Complexity limit test**: an expression built to exceed the configured
  node/depth limit is rejected at `Compile()`, not silently accepted and
  slow at `Evaluate()`.
- **Timeout test**: register a custom function that sleeps past a short
  `ctx` deadline; confirm `Evaluate()` returns `ErrEvaluationTimeout`
  (or the equivalent `ctx.Err()`-wrapped error) rather than hanging or
  ignoring the deadline.
- **Concurrent Compile()/Evaluate() stress test**: many goroutines
  compiling overlapping and distinct expressions while other goroutines
  concurrently evaluate already-compiled policies — confirm no race
  (`-race` mandatory) and no cache corruption.
- **Type-coercion edge cases**: numeric type mismatches, missing attribute
  keys, nil handling — per the rules decided in §5.

---

## 10. Dependencies

- Whichever expression-parsing library is chosen in §2.3
  (`expr-lang/expr` or `google/cel-go`) — used only for parsing, per §2.2.
- `grlog` — optional structural `Logger`, matching every sibling.
- No dependency on grcache, grevents, graudit, or gourdiantoken. grpolicy is
  meant to be the most dependency-light repo after grconfig (which doesn't
  exist yet) — a pure computation engine.

---

## 11. Roadmap (v0.2+)

- Nested/hierarchical attribute paths (`user.address.city`).
- A policy-linting/validation CLI tool.
- Performance: precompiled policy "warm cache" pre-population, if `grauth`
  usage patterns reveal cold-compile latency matters in practice.

---

## 12. Evaluation questions for the agent

1. Having read all five sibling repos (§0), which conventions (Close()
   pattern, sentinel-error style, Logger interface, conformance suite
   structure) are directly reusable, and which (if any) don't fit
   grpolicy's shape and need a documented, justified deviation — the same
   way grcache's subpackage-per-backend split was a documented deviation
   from gourdiantoken/grlog's flat structure?
2. `expr-lang/expr` vs `google/cel-go` — final decision with reasoning,
   specifically addressing AST accessibility for a custom tree-walking
   evaluator (§2.2/§2.3).
3. Short-circuit trace behavior (§0.3/§9) — final decision, with the
   proving test shown.
4. Missing-attribute-key and type-mismatch behavior (§5) — final decision,
   with tests shown.
5. Is the compiled-policy cache (§7) verified safe under real concurrent
   load, not just assumed safe from a static read of the locking code?
6. Does the evaluator actually respect `ctx` cancellation mid-evaluation
   (checking `ctx.Done()` between sub-expression evaluations), or only at
   the very start of `Evaluate()` — show the actual timeout test passing.