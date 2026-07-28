# Add `*ast.MemberNode` / `*ast.ChainNode` support to grpolicy

## Context

grpolicy's evaluator (`eval.go`) has no case for `*ast.MemberNode` — the AST
node `expr-lang/expr`'s parser produces for both dotted (`resource.ownerID`)
and bracket (`resource["ownerID"]`, `items[0]`) access. Every such expression
compiles successfully today (no compile-time rejection) but always fails at
`Evaluate` with `"grpolicy: unsupported expression node *ast.MemberNode"`.
This is a real, named gap: `docs.go`, `README.md`, and
`docs/plan/grpolicy-plan.md` all explicitly call flat-map-only attributes a
"v0.2+ candidate" limitation, and it blocks the exact ABAC ownership-check
pattern (`resource.ownerID == subject.id`) grpolicy's own ecosystem docs use
as the canonical example of how `grauth` would consume it.

This is not a hypothetical pre-1.0 feature in a vacuum: `grauth` (a sibling
repo, already built, **not** "not yet built" as grpolicy's own README/CLAUDE.md
currently claim — that line is now stale) is a real consumer pinned to
`github.com/gourdian25/grpolicy v0.2.0`, and its authorizer
(`internal/features/authz/authorizer.go:154`) already does
`errors.Is(evalErr, grpolicy.ErrAttributeMissing)` → treat as deny. grauth's
own attrs today are 100% flat `map[string]any` of primitives — no nested
maps/structs flow through it yet — so this change is purely additive from
grauth's side (new capability, zero existing behavior changed), and reusing
`ErrAttributeMissing`/`ErrTypeMismatch` for every new failure mode (rather
than inventing new sentinels) means grauth's existing deny-on-missing-attr
handling covers the new nested-access failures with **zero grauth-side
changes required**.

**Scope, confirmed with the user (not to be re-litigated):**
1. Struct field access via `reflect` — in scope.
2. Optional chaining (`a?.b`, `a?.[0]`, multi-link chains) — in scope.
3. Both string map-key access (`resource["ownerID"]`) and numeric slice/array
   index access (`items[0]`, `items[i]`) — in scope, same code path (they're
   the same AST node, `*ast.MemberNode`).

Explicitly **out of scope**: method-call syntax (`a.b()` — already rejected
at Compile time today via `evalCall`'s non-identifier-callee check, stays
rejected, untouched), slicing (`a[1:2]`, a distinct `*ast.SliceNode`), string
indexing (`name[0]` where `name` is a Go `string`).

Empirically verified against the real `expr-lang/expr@v1.17.8` source (not
assumed — matching this repo's own `docs/architecture.md` §9/§11 discipline):
`ast.MemberNode{Node, Property, Optional, Method}` covers both dot and
bracket syntax (`Property` is always `*ast.StringNode` for dot access, an
arbitrary sub-expression for bracket access); `ast.ChainNode{Node}` wraps a
run of one-or-more `?.`-containing links. Traced `parser.go`'s
`parsePostfixExpression` line-by-line: once any `?.` appears in a postfix
chain, the parser wraps the **entire accumulated chain so far** (including
earlier plain-`.` links) in one `ChainNode`, re-wrapping at every subsequent
step — a link written **before** the first `?.` is never inside any
`ChainNode`. Confirmed this is load-bearing, not cosmetic, by reading the
compiler (`compiler/compiler.go:682-736`): `OpJumpIfNil` is only emitted for
an `Optional`-flagged `MemberNode` when `len(c.chains) > 0` (i.e. only for
links reachable inside a `ChainNode`) — so the nil-guard is genuinely
asymmetric around the first `?.`, not just an artifact of how the AST prints.
grpolicy's own evaluator must replicate this exact asymmetry to stay faithful
to what the syntax actually means, using a purely static/structural check
(no runtime flag threading needed) since "has `?.` been used at or before
this point" is fully knowable from the AST shape alone.

`ast.MemberNode.Location()`/`ast.ChainNode.Location()` both cover only a
fragment (same class of bug `docs/architecture.md` §11 already fixed once
for `CallNode`/`BuiltinNode`/`ArrayNode`): dot-form is fixed for free once
`Property` is added to `children()` (plain span-union already works); bracket
form needs the same `matchClosingFrom`-style fix as `CallNode`/`ArrayNode`.

## Stage list (tracker)

| Stage | Goal | Status |
|---|---|---|
| 1 | `children()`/`nodeSpan` foundation for `MemberNode`/`ChainNode` — fixes latent complexity-undercounting gap; `Evaluate` behavior unchanged | Done |
| 2 | `evalMember` over `map[string]any`, general maps, slices/arrays — dot **and** bracket syntax, no structs, no `?.` yet | Done |
| 3 | `evalMember` extended to Go structs via `reflect` (pointer deref, exported-field lookup, panic-recovery backstop) | Done |
| 4 | `*ast.ChainNode` + `MemberNode.Optional` — asymmetric short-circuit semantics, transparent `ChainNode` trace | Done |
| 5 | Finalize: version bump, CHANGELOG, README/docs.go/architecture.md closing pass, full regression + longer fuzz run | Not started |

Each of stages 1–4 updates its own slice of `README.md` / `docs.go` /
`docs/architecture.md` §12 (new) / `CHANGELOG.md` / `fuzz_test.go` seeds in
the same stage — docs are not deferred to one giant stage at the end. We
implement and review one stage at a time; do not start a stage until the
previous one is reviewed and approved.

## Key design decisions

**Sentinel errors: reuse `ErrAttributeMissing`/`ErrTypeMismatch`, add none.**
Matches `docs/architecture.md` §8's discipline ("only two ever added, each
for a specific hard requirement") and the concrete grauth precedent above.

- `ErrAttributeMissing` = "the specific thing referenced does not exist":
  missing map key, out-of-range slice/array index (including negative), nil
  base reached without an enclosing `?.`, struct field not found or found but
  unexported (never panics, never silently treated as present).
- `ErrTypeMismatch` = "the shape is structurally nonsensical for this
  operator": non-string property against a map, non-numeric property against
  a slice/array, non-string property against a struct, or any base kind
  grpolicy doesn't support member access on at all (this is where plain `string`
  bases are rejected, satisfying the string-indexing exclusion via the same
  generic "unsupported base kind" branch as anything else — not a bespoke check).

**`CheckNode` shape: `Children = [nodeCheck, propertyCheck]` uniformly for
dot and bracket forms.** This falls out for free once `children()` returns
`[node.Node, node.Property]`: a dot-form `Property` is always a trivially-
evaluable `*ast.StringNode` leaf (existing `case *ast.StringNode` in `eval()`
already handles it, zero new code), bracket-form `Property` is evaluated by
the same generic `w.eval(node.Property)` recursion (it may itself be an
identifier, arithmetic expression, nested member access, etc.). `evalMember`
mirrors `evalComparison`'s exact pattern: eval base, propagate error; eval
property, propagate error; resolve; only on full success set
`Evaluated/Result/Passed`. No new case needed in `operatorOf` (`MemberNode`/
`ChainNode` get `Operator == ""`, same as `CallNode`/`ArrayNode` today).
**`ChainNode` is fully transparent in the trace** — `evalChain` returns
exactly what `evalMember` returns for its wrapped node; no `ChainNode` row
ever appears in `Decision.Trace`.

**Reflection safety.** Guard, don't just recover: `field.CanInterface()`
checked *before* ever calling `.Interface()` (never call-then-catch);
`isNilBase(v any) bool` handles the nil-interface-vs-typed-nil-pointer trap
(`v == nil || (Ptr/Map/Slice/Chan/Func/Interface kind && reflect.ValueOf(v).IsNil())`),
checked uniformly before any kind-specific resolution; numeric/key
convertibility checked via `AssignableTo`/`ConvertibleTo` before ever calling
`MapIndex`/`Index`/`Convert` (mirrors `functions.go`'s existing
`isNumericKind`+`Convert` pattern). A targeted `recover()` around just the
resolution helper's body is the backstop for anything the checks miss —
mirrors `functionRegistry.call`'s existing precedent (`functions.go:142-147`)
exactly, including its convention that a recovered panic is a **plain
diagnostic error, not a wrapped sentinel** (a panic here means grpolicy's own
pre-checks had a gap — mislabeling it as an ordinary policy-authoring mistake
would mislead the caller).

**Optional chaining — two decisions:**
- **A: `?.` guards against nil, never against absence.** A missing map key,
  out-of-range index, or missing/unexported struct field is *always*
  `ErrAttributeMissing`, chain or no chain — `?.` only suppresses a
  successfully-resolved value that is itself nil. Preserves the one
  non-negotiable rule (missing always fails loud) with no loophole through
  new syntax.
- **B: once the first `?.`/`?.[` is hit walking outward from the base, every
  subsequent link tunnels a nil through to the end, regardless of that
  link's own `.Optional` flag; a link before the first `?.` is never
  protected.** Confirmed as real expr-lang/VM semantics (not invented), via a
  purely static predicate `inOptionalScope(node *ast.MemberNode) bool` (true
  if `node.Optional`, or recursively true for `node.Node` when it's itself a
  `*ast.MemberNode`) — no runtime flag threading needed. On a nil base:
  `Property` is *never* evaluated (proves no partial evaluation past the nil
  — e.g. `a?.b[expensiveIndex()]` must never call `expensiveIndex()` when `a`
  is nil); the short-circuited node itself is `Evaluated:true, Result:nil,
  Passed:false` (per `CheckNode.Result`'s own doc comment: "a nil Result with
  Evaluated == true is possible only for a node whose value is legitimately
  nil" — a tunneled-to-nil link is exactly that), with its `Property` child
  stubbed via the existing `buildStub`.

**Compile-time validation: no new checks needed.** `measureComplexity` and
`validateFunctionCalls` (engine.go) are purely `children()`-driven recursions
with zero node-specific logic — adding `MemberNode`/`ChainNode` to
`children()` is necessary and sufficient for both. `attrs` is runtime-only,
so there's no way to statically know a member chain is valid before
`Evaluate` — no new Compile-time rejection is warranted.

**Fuzz corpus needs explicit new seeds** — zero existing seeds in
`fuzz_test.go` contain `.`/`[...]` member-access syntax; each stage adds its
own seeds and extends the fixed test-attrs map (nested maps in stage 2, a
struct with an exported+unexported field in stage 3, `?.` seeds in stage 4).

## Per-stage detail

### Stage 1 — `children()`/`nodeSpan` foundation
- `ast_helpers.go` `children()`: add `*ast.MemberNode → [node.Node, node.Property]`, `*ast.ChainNode → [node.Node]`.
- `ast_helpers.go` `nodeSpan()`: add `*ast.ChainNode → nodeSpan(node.Node, source)` (transparent delegation, added to the existing early-return switch). Add `*ast.MemberNode` handling: compute base's span, scan forward (skipping whitespace) — if next byte is `[`, bracket form → `matchClosingFrom` from there (same as `ArrayNode`); otherwise fall through to the existing generic children-span-union path (already correct for dot form once `children()` includes `Property`).
- `eval()`'s switch, `errors.go`: untouched — `MemberNode`/`ChainNode` still hit `default:` in `eval()`.
- Tests: `internal_coverage_test.go` white-box tests for `children()`/`nodeSpan` on hand-built `MemberNode`/`ChainNode` ASTs (dot form, bracket form incl. closing-bracket regression, transparent ChainNode span). `coverage_test.go` public-API test proving the complexity-undercounting fix: `WithMaxComplexity(3, 100)` + `Compile("a.b.c.d.e")` must now fail `ErrTooComplex` (would silently succeed today). Regression: existing `TestCompile_MemberCallTargetUnsupported` must keep passing unchanged.
- Docs: none user-visible yet; open `docs/architecture.md` §12 with the empirical AST-shape findings only.

### Stage 2 — maps, slices, arrays (dot + bracket)
- `eval.go`: `case *ast.MemberNode: return w.evalMember(node)` in `eval()`.
- New `evalMember(node *ast.MemberNode) (any, CheckNode, error)`: eval base (recursing into `evalMember` if `node.Node` is itself a `MemberNode`, else `w.eval`); propagate error; if `isNilBase(baseVal)` → hard-fail `ErrAttributeMissing` (no `?.` exists yet — this becomes the "no optional scope" branch of stage 4's fuller rule); else eval `Property`, propagate error, call `resolveMember(baseVal, propVal, node)`.
- New `resolveMember(base, propVal any, node *ast.MemberNode) (any, error)`: fast path `map[string]any` + string key; else `reflect.ValueOf(base)` + `switch rv.Kind()`: `Map` (convert key via `AssignableTo`/numeric `ConvertibleTo`, `ErrAttributeMissing` if absent, `ErrTypeMismatch` if unconvertible), `Slice`/`Array` (convert index via new `toInt` — integer kinds only, no float truncation — `ErrAttributeMissing` if out of range incl. negative, `ErrTypeMismatch` if unconvertible), `default: ErrTypeMismatch` (covers plain `string` bases and anything else unsupported).
- New `isNilBase(v any) bool`, `toInt(v any) (int, bool)` (mirrors `toFloat`'s exhaustive integer-kind switch, excluding floats deliberately).
- Tests: new `testMemberAccessCollections` in `contract_engine_test.go` (registered in `runEngineContract`) covering: dot/bracket produce identical result+Children shape; missing map key (mirrors `testMissingAttributeKey`'s exact assertion shape); non-string key against map → `ErrTypeMismatch`; slice and array index access; out-of-range index → `ErrAttributeMissing` not a panic; non-integer index → `ErrTypeMismatch`; nil map/slice base (no `?.`) → `ErrAttributeMissing`; string base indexing → `ErrTypeMismatch` (proves the exclusion); 3-level nested chain. Extend `testExpressionTextIncludesClosingDelimiters` with a bracket-form-through-the-public-API case.
- Docs: `README.md` "Expression language" (replace the "v0.2+ candidate" line with what's now supported/still not), `docs.go`, `CHANGELOG.md` new `[0.3.0]` section opened, `docs/architecture.md` §12 sentinel-bucketing rule, `fuzz_test.go` new `nested` attr + seeds.

### Stage 3 — struct fields via `reflect`
- `resolveMember`: add `case reflect.Struct` after making the pointer-deref loop load-bearing (dereference through pointers, checking `IsNil()` at each level): `propVal` must be a string (else `ErrTypeMismatch`); `field := rv.FieldByName(name)`; invalid or `!field.CanInterface()` → `ErrAttributeMissing` (unexported never panics, never exposed); else `field.Interface()`.
- Add the `recover()` backstop to `resolveMember`, mirroring `functionRegistry.call`'s precedent exactly (plain diagnostic error, no sentinel).
- Tests: new `testMemberAccessStructFields` — exported field dot+bracket; **unexported field fails loud, not panics** (the single most important test here — reaching the assertion at all is part of the proof, same framing as this repo's existing custom-function-panic-recovery test); unknown field name; pointer-to-struct auto-dereferenced; nil pointer base (no `?.`) → `ErrAttributeMissing` not a panic; embedded/promoted field reachable; non-string property against struct → `ErrTypeMismatch`. `internal_coverage_test.go`: white-box test proving the `recover()` branch is real (constructing a case that reaches it despite the pre-checks).
- Docs: `README.md` (case-sensitive exact-Go-field-name requirement, methods still unsupported), `docs.go`, `CHANGELOG.md`, `docs/architecture.md` §12, `SECURITY.md` scope-notes bullet (reflection reads only exported fields, never invokes methods, unexpected panics recovered — same posture as the existing custom-function bullet), `fuzz_test.go` struct attr + seeds.

### Stage 4 — `ChainNode` + optional chaining
- `eval.go`: `case *ast.ChainNode: return w.evalChain(node)` in `eval()`.
- New `evalChain(node *ast.ChainNode) (any, CheckNode, error)`: type-assert its child to `*ast.MemberNode`, delegate fully transparently to `evalMember` (no wrapping `CheckNode`); defensive fallback to `w.eval(node.Node)` for any other shape (matches this repo's existing defensive-fallback style).
- New `inOptionalScope(node *ast.MemberNode) bool` — pure, static, structural (Decision B above).
- `evalMember`: replace stage 2's unconditional nil-base hard-fail with the full three-way branch (short-circuit to `Evaluated:true,Result:nil,Passed:false` with `Property` stubbed when `inOptionalScope(node)`; hard-fail `ErrAttributeMissing` otherwise). `resolveMember` untouched.
- Tests: new `testOptionalChainShortCircuit`, mirroring `testShortCircuitAndTrace`'s exact falsifying-test discipline (call-counter + trace-shape, verified to actually fail if short-circuiting were disabled): nil base short-circuits a single `?.` link; nil intermediate short-circuits remaining links **without evaluating them** (register a counting function inside a bracket index, assert zero calls); a link *before* the first `?.` is NOT protected (hard error); plain `.` links *after* the first `?.` still tunnel; a present-but-missing key under `?.` still fails loud (proves Decision A); a genuinely-nil stored value tunnels identically to a short-circuit.
- Docs: `README.md` (state Decision A/B precisely — this is exactly the kind of "precise, non-aspirational claim" README.md prizes elsewhere), `docs.go`, `CHANGELOG.md`, `docs/architecture.md` §12 final additions (Decision A, Decision B, the "Evaluated:true on tunnel" trace decision — each naming its falsifying test), `fuzz_test.go` `?.` seeds.

### Stage 5 — finalize (not a release)
- `version.go`: `v0.2.0` → `v0.3.0` (backward-compatible additive feature).
- `CHANGELOG.md`: finalize `[0.3.0]` date, consolidate bullets from stages 1–4.
- `README.md` "Out of scope": remove the stale "nested paths... v0.2+ candidate" line, replace with what's *still* out of scope (method calls, slicing, string indexing). Also fix the stale "grauth (not yet built)" framing in README.md while touching this section, since it's now demonstrably wrong.
- `docs/plan/grpolicy-plan.md`: append an annotation (don't delete — historical record) noting this was implemented in v0.3.0, pointing at `CHANGELOG.md`/`docs/architecture.md` §12.
- Full regression: existing call-target/unsupported-node-kind tests unchanged; `make race`; `make coverage-check` (≥95%); `make fuzz` with an extended `FUZZTIME` given four stages of new code paths.
- Explicitly **not** in scope: `make release`/tagging — a separate, later, human-approved step.

## Critical files

- `eval.go` — `eval()`'s type switch, `evalMember`/`evalChain`/`resolveMember`, `isNilBase`/`toInt`/`inOptionalScope`
- `ast_helpers.go` — `children()`, `nodeSpan()`, `matchClosingFrom()`
- `contract_engine_test.go` — `runEngineContract`, new `testMemberAccess*`/`testOptionalChainShortCircuit` functions
- `errors.go` — no new sentinels, just confirming reuse
- `functions.go` — `functionRegistry.call`'s `recover()` is the precedent `resolveMember`'s backstop mirrors
- `docs/architecture.md` — new §12
- `fuzz_test.go` — seed corpus + attrs map extensions per stage
- `internal_coverage_test.go` — white-box tests for branches unreachable from the public API alone

## Verification (per stage)

- `go test . -run TestEngine_Contract/<NewScenarioName> -v` for the stage's new contract scenario(s)
- `make race` (mandatory — this touches `eval.go`)
- `make coverage-check` (≥95% gate)
- `make lint` / `make vet`
- Stage 5 only: `make fuzz FUZZTIME=10m` (or similar) as a final cross-stage check
