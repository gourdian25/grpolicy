# grpolicy architecture decisions

This document records specific, narrow decisions someone might otherwise
"fix" incorrectly later — not a general design overview. Each heading names
a decision, states what the "obvious" or sibling-consistent alternative
would have been, and explains precisely why grpolicy departs (or doesn't).

## 1. `expr-lang/expr` chosen over `google/cel-go`

grpolicy needs a parser that gives direct access to a walkable AST, because
`Decision.Trace` is built by grpolicy's own evaluator as it walks that AST —
not derived after the fact from a third-party library's execution result.
`expr-lang/expr`'s `parser.Parse` returns a plain Go struct hierarchy
(`ast.BinaryNode`, `ast.CallNode`, etc., type-switchable directly) with no
forced adoption of a separate type system. `google/cel-go`'s AST is
protobuf/oneof-shaped and normally paired with its own mandatory
type-checker (`env.Check`) — more ceremony to bypass cleanly than
expr-lang's, for a benefit (Kubernetes-admission-control-scale battle
testing) grpolicy doesn't need: grpolicy enforces its own attribute
coercion/type rules in `eval.go` regardless of which parser is used.
`expr-lang/expr`'s Go-idiomatic syntax also reduces friction for the Go
developers who will author policies via the future `grauth` integration.
cel-go remains the documented runner-up, not dismissed lightly — re-litigate
this only if a real requirement for CEL's specific semantics (e.g. protobuf
attribute schemas) emerges.

## 2. Only `parser.Parse` is imported — never `expr.Compile`/`Run`/`vm.Program`/`expr.Function`

grpolicy imports exactly two things from the whole `expr-lang/expr` module
tree: `github.com/expr-lang/expr/parser` (for `parser.Parse`) and
`github.com/expr-lang/expr/ast` (for the node types) — plus, for one narrow
purpose described in §5 below, `github.com/expr-lang/expr/builtin`'s
`Index` map. Nothing else. This means the AST grpolicy walks is
unoptimized and unchecked by expr's own passes (no constant folding, no
static type inference) — treated here as a feature, not a limitation: a
literal stays a literal node, an identifier stays an identifier node, and
`CheckNode`'s shape is exactly what the policy author wrote, not something
expr's optimizer decided to fold away.

## 3. Flat package, no subpackage split

Unlike `grcache`/`graudit`'s per-backend subpackage layout (which exists
because each storage backend pulls in a different client library
dependency), grpolicy has exactly one evaluation strategy. A hypothetical
"subpackage per built-in function set" was considered and rejected: built-in
functions are additive registry entries (`functions.go`) or `BuiltinNode`
cases (`eval.go`), not alternate implementations of the `Engine` interface —
there is nothing to split along.

## 4. Short-circuit trace policy

`Decision.Trace` shows the *full* expression shape as written, including
`&&`/`||` branches that were short-circuited and never actually ran. An
unevaluated node is marked `Evaluated: false` with `Result` left `nil` —
never a coerced `false`. A `CheckNode` with `Evaluated: true, Result: false`
means "this ran and returned false"; the two states are never conflated.
This is chosen over "only show branches that ran" because a caller debugging
*why* a decision was DENY usually needs to see the whole authored policy
shape, including what was skipped, not just what fired. Proven (not just
implemented) by `conformance.testShortCircuitAndTrace`, which asserts both a
call counter of zero *and* the correct `CheckNode` shape — and was verified
to actually fail when short-circuiting was temporarily disabled during
development, not just assumed to catch a regression.

## 5. Missing-key / type-mismatch: fail loud, with a partial trace

A missing attribute key or an operator applied to incompatible types returns
`ErrAttributeMissing`/`ErrTypeMismatch` — never a silently substituted zero
value or coerced comparison. `Evaluate`'s existing `(Decision, error)`
signature is sufficient for this: Go's `(value, err)` idiom (the same shape
as `io.Reader`) means a non-nil error can carry a non-zero `Decision.Trace`
alongside it, populated with every `CheckNode` successfully built before the
failure point — no interface change was needed. This was a genuinely open
question in the original design and is confirmed resolved, not left
ambiguous. Both failure modes are proven by dedicated falsifying tests
(`testMissingAttributeKey`, `testTypeMismatch`) that were verified to
actually fail when the fail-loud behavior was temporarily replaced with
silent substitution during development.

## 6. `conformance/`'s reshaped purpose here

In `grcache`/`graudit`, `conformance/` exists to prove multiple backend
implementations behave identically through one shared interface. grpolicy
has exactly one implementation — there is no backend-parity concern to
prove. `conformance.Run` is kept as a reusable entry point anyway, but its
purpose is simply "grpolicy's own primary behavioral suite," callable from
the root package's own tests and, potentially, from a future `grauth`
integration test. `RunOption` is kept as an unused extension point (matching
the sibling shape) rather than removed, since a future scenario-specific
relaxation may need it — but zero options are actually defined at v0.1.0.

## 7. RE2-only regex guarantee

The `matches` operator only ever uses Go's standard-library `regexp`
package, which is RE2-based and therefore immune to catastrophic
backtracking (ReDoS) by construction — confirmed by a conformance test that
runs a classically-pathological pattern (`(a+)+$`) against adversarial input
and asserts completion well under a timeout, rather than merely asserting
"no alternate regex library is imported." This is a security property, not
an implementation detail: any future change to a different regex engine
requires re-litigating this document.

## 8. Two sentinel errors added beyond the original spec list

`ErrAttributeMissing` and `ErrClosed` were added to the sentinel list beyond
what the original design draft enumerated. `ErrAttributeMissing` is required
by decision §5 above. `ErrClosed` is required by the `Close()`-idempotency
contract every sibling repo follows (`sync.Once`/`atomic.Bool`-guarded,
returns a defined error for any call after `Close`) — even though v0.1.0's
`Close()` has nothing real to release yet, the sentinel and the pattern are
kept so a future cache-sweeper or metrics-flush goroutine can be added
without a breaking API change.

## 9. `RegisterFunction` rejects names reserved by expr-lang/expr's own grammar

Discovered empirically while building `eval.go` (via `go doc` and real
`parser.Parse` output, not assumed): expr-lang/expr's parser reserves a
fixed set of bare identifiers — enumerated in
`github.com/expr-lang/expr/builtin.Index` — and always parses a call to one
of them (e.g. `now()`, `duration()`, `len()`, `all()`) as an
`*ast.BuiltinNode`, never an `*ast.CallNode`, regardless of whether a
same-named function has been registered. This means a function registered
under a reserved name via `RegisterFunction` could never actually be
reached — calls to that name would always route to the parser's reserved
meaning (or fail as unsupported) instead.

grpolicy handles this in two parts:

1. **`now` and `duration`** are implemented directly as grpolicy's own
   `*ast.BuiltinNode` handlers in `eval.go` (`now()` returns `time.Now()`;
   `duration(s)` wraps `time.ParseDuration`) — satisfying the spec's
   "now() and date/duration helpers" requirement through the only code path
   that can actually reach them.
2. **`RegisterFunction` rejects any other reserved name outright**, with a
   clear error explaining why, rather than silently accepting a registration
   that could never be invoked. This is the same fail-loud philosophy as
   decision §5 applied to a different failure mode: a confusing runtime
   mismatch (registered but unreachable) is worse than an immediate,
   explicit rejection at registration time.

`before`, `after`, and `addDuration` — grpolicy's other seeded built-ins —
are not reserved names and are registered normally through the
`functionRegistry`, reached via ordinary `*ast.CallNode` dispatch.
