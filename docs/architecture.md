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
implemented) by `testShortCircuitAndTrace`, which asserts both a
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

## 6. No separate `conformance/` package

In `grcache`/`graudit`, `conformance/` exists to prove multiple backend
implementations behave identically through one shared interface. grpolicy
has exactly one implementation — there was never a backend-parity concern
to prove. It briefly existed anyway as its own importable package (so a
future `grauth` integration test could reuse it), but was folded into the
root package's own `contract_engine_test.go` (`runEngineContract`, run via
`TestEngine_Contract`) for consistency with the rest of the gourdian
ecosystem's convention of keeping shared behavioral suites as root-package
test files rather than separately importable packages — `runEngineContract`
still takes a `newEngine` constructor function, so a future consumer could
still drive it by copying the pattern, just not by importing it directly.
The vestigial `RunOption`/`runConfig` extension point (zero options ever
defined) was dropped in the same pass rather than carried forward, since
nothing outside the module could construct one anyway once unexported.

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
2. **`RegisterFunction` rejects *every* reserved name outright — including
   `now` and `duration` themselves**, not just the ones grpolicy doesn't
   implement. An earlier version of this code treated `now`/`duration` as an
   exception and allowed registering them, on the theory that grpolicy
   "owns" those two names — but that was backwards: a registered `now`
   function is exactly as unreachable as a registered `len` function, since
   the parser produces a `BuiltinNode` for either one regardless of what
   grpolicy itself does internally. This was caught by a hardening test
   (`FunctionRegistryEdgeCases/ReservedBuiltinNamesRejected`) that asserted
   registration of `now` should fail, and it initially didn't — a concrete
   example of why the fail-loud registration check needs its own test
   rather than being trusted by inspection. The error message distinguishes
   the two cases for the caller: "grpolicy already implements this
   directly" for `now`/`duration`, vs. "grpolicy doesn't support this
   reserved name" for everything else in `builtin.Index`.

`before`, `after`, and `addDuration` — grpolicy's other seeded built-ins —
are not reserved names and are registered normally through the
`functionRegistry`, reached via ordinary `*ast.CallNode` dispatch.

## 10. Compiled-policy cache key is the raw, unnormalized source string

`Compile`'s cache (`cache.go`) is keyed by the exact `expr` string passed
in — no whitespace collapsing, no AST-based canonicalization. `"a && b"` and
`"a&&b"` are semantically identical but produce two separate cache entries
and two distinct `CompiledPolicy` values; calling `Compile` twice with
byte-identical text returns the same cached value. Neither behavior
(raw-string keying vs. semantic-equivalence keying) is wrong in the
abstract, but it must be a deliberate, documented, tested choice rather than
an accidental side effect of "we happened to use the string as a map key" —
that is the whole point of writing it down here. Raw-string keying was
chosen for v0.1.0 because it requires no extra work at `Compile` time (no
AST-normalization pass, no canonical pretty-printer) and because grpolicy's
anticipated caller (`grauth`) is expected to compile a fixed, small set of
policy strings defined once in configuration — not a high-cardinality stream
of dynamically-assembled, whitespace-varying expressions where semantic
deduplication would meaningfully reduce cache pressure. If that assumption
turns out to be wrong in practice, semantic-equivalence keying (e.g. hashing
a normalized re-print of the AST) is a valid v0.2+ change, but it changes
`WithCacheSize`'s effective capacity semantics and must be re-documented
here if adopted. Proven by
`testCacheKeyIsRawSourceString`.

## 11. `CheckNode.Expression` for calls and array literals: bracket-matching, not source-slicing

Found by actually running `example/example.go` (not by any automated test —
this is a caution against trusting a green test suite alone when the suite
never asserted the exact string in question): `isBusinessHours(hour)`'s
`CheckNode.Expression` came back as `"isBusinessHours(hour"` — missing the
trailing `)`. The cause: `ast.CallNode.Location()` and
`ast.BuiltinNode.Location()` cover only the bare function name (confirmed
empirically, same as the `BinaryNode`-covers-only-its-operator finding in
§0 of the original plan), and no argument's own span reaches the closing
`)` either — it belongs to no node at all in expr-lang/expr's AST, so
unioning children's spans (`nodeSpan`'s original approach, correct for
`BinaryNode`/`UnaryNode`) systematically misses it. The same issue affects
`ArrayNode` and its trailing `]`.

The fix (`ast_helpers.go`'s `nodeSpan`/`matchClosingFrom`): for
`CallNode`/`BuiltinNode`/`ArrayNode` specifically, locate the opening
`(`/`[` from the name's end (or, for `ArrayNode`, its own `Location()`,
which does start at `[`) and scan forward for the balanced matching close,
tracking nested brackets of either kind uniformly and skipping over
double-quoted string contents so a bracket character inside a string
literal argument is never miscounted. Every other node kind keeps the
original children-span-union approach, which is sufficient for them.

Added as a regression test,
`testExpressionTextIncludesClosingDelimiters`, covering a
zero-arg call, a one-arg call, a nested call, and an array literal — the
exact shapes that silently lost their closing delimiter before the fix.
The lesson generalizes: `Decision.Trace` is a debugging/explainability
surface whose entire value proposition is showing a human exactly what ran
— a subtly-wrong `Expression` string is the kind of bug automated
assertions on `Allowed`/`Result`/`Evaluated` alone will never catch, since
it doesn't affect the boolean outcome at all. Running the actual example
program, not just the test suite, is what caught it.

## 12. Nested attribute access — `*ast.MemberNode`/`*ast.ChainNode` support

Tracked stage-by-stage in `docs/plan/member-access-support-plan.md`. This
section accumulates the decisions made along the way; entries are added as
each stage lands, not all at once up front.

**AST shape, confirmed empirically against `expr-lang/expr@v1.17.8`'s actual
source (not assumed), same discipline as §9/§11:**

```go
type MemberNode struct {
	base
	Node     Node // base being accessed, e.g. "foo" in "foo.bar"
	Property Node // dot access: always *ast.StringNode holding the literal
	              // field name text. Bracket access "foo[X]": Property is X
	              // itself, a fully general expression (*ast.StringNode for
	              // foo["b"], *ast.IntegerNode for foo[0], *ast.IdentifierNode
	              // for foo[i], etc.)
	Optional bool // true for "foo?.bar" / "foo?.[bar]"
	Method   bool // true only when this MemberNode is a CallNode's Callee
}
```

One node kind covers *both* dot and bracket access — there is no separate
`IndexNode`. `*ast.ChainNode{Node}` wraps a run of one-or-more
`?.`-containing links.

**`Location()` is a fragment for both, same class of bug as §11:** dot form's
`MemberNode.Location()` covers only the property name (identical to
`Property.Location()`); bracket form's covers only the bare `[`. Neither is
useful directly. The fix, in `ast_helpers.go`'s `children()`/`nodeSpan`:

- `children()` gained `*ast.MemberNode → [Node, Property]` and
  `*ast.ChainNode → [Node]`. This is necessary *and* sufficient for
  `measureComplexity` and `validateFunctionCalls` (both purely
  `children()`-driven recursions, confirmed by reading `engine.go` in
  full) to handle both kinds correctly with zero node-specific code of
  their own — proven by `TestComplexity_MemberChainNodesAreCounted`
  (`coverage_test.go`), which shows a real, previously-latent bug this
  fixes: before this change, `children()` returned `nil` for `MemberNode`
  (the generic leaf default), so a dotted chain of *any* depth counted as
  exactly one opaque leaf node — `WithMaxComplexity`'s node-count limit
  silently didn't apply to member-access depth at all, harmless only
  because `Evaluate` unconditionally rejected every `MemberNode` anyway.
- `nodeSpan`'s dot-form span falls out **for free** from the existing
  generic children-span-union path once `Property` is in `children()` — no
  bracket-matching needed, because the `.` separator sits inside the gap
  the union already spans (proven by `TestNodeSpan_MemberNodeDotForm`,
  covering both a single link and a multi-link chain).
- `nodeSpan`'s bracket form needs the *same* `matchClosingFrom`-style fix §11
  already applied to `CallNode`/`BuiltinNode`/`ArrayNode` — the naive union
  would miss the trailing `]` (proven by `TestNodeSpan_MemberNodeBracketForm`).
  Since `MemberNode` has no field distinguishing dot-vs-bracket syntax, the
  two are told apart *structurally*: scan forward from the base's span for a
  `[` immediately following it; if `matchClosingFrom` finds one there (its
  return value differs from its input), it's bracket form; otherwise fall
  through to the generic dot-form path. No new AST inspection needed beyond
  what `matchClosingFrom` already provides.
- `*ast.ChainNode` is a transparent wrapper for span purposes too — its own
  `Location()` also covers only a fragment of the chain (confirmed
  empirically, e.g. `a?.b`'s `ChainNode.Location()` is just `"b"`'s span), so
  `nodeSpan` resolves it as exactly its wrapped node's own (already-correct)
  span (proven by `TestNodeSpan_ChainNodeTransparent`).

Traced `parser/parser.go`'s `parsePostfixExpression` line-by-line (relevant
for a later stage's optional-chaining semantics, recorded here since it was
discovered while researching this section): once any `?.` appears in a
postfix chain, the parser wraps the **entire accumulated chain so far**
(including earlier plain-`.` links) in one `ChainNode`, re-wrapping at every
subsequent step — a link written *before* the first `?.` is never inside any
`ChainNode`. Confirmed load-bearing (not cosmetic) by reading
`compiler/compiler.go`'s `MemberNode`/`ChainNode` compilation:
`OpJumpIfNil` is only ever emitted for an `Optional`-flagged `MemberNode`
when `len(c.chains) > 0`, i.e. only for links reachable inside a `ChainNode`
— so expr-lang's own nil-guard is genuinely asymmetric around the first
`?.`, not just an artifact of how the tree prints. A later stage's
`evalMember`/`evalChain` must replicate this exact asymmetry to stay
faithful to what the syntax actually means.

At this stage, `eval()` still has no case for `*ast.MemberNode`/
`*ast.ChainNode` — both still hit the existing "unsupported expression node"
default. Evaluation support, the sentinel-error bucketing decision, and the
optional-chaining short-circuit semantics are recorded in this section as
later stages land.
