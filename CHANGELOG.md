# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

Ecosystem-wide Stage 2 pass (test structure + a couple of real bug fixes +
coverage): no breaking API changes.

### Changed (breaking)

- `Logger`'s three printf-style methods (`Infof`/`Warnf`/`Errorf(format
  string, args ...interface{})`) replaced with four `log/slog`-shaped
  methods (`Debug`/`Info`/`Warn`/`Error(msg string, args ...any)`), matching
  `*slog.Logger`'s own signatures exactly so any slog-based logger —
  including `*grlog.Logger` via `slog.New(grlog.NewSlogHandler(...))` —
  satisfies it with no adapter. Allowed pre-1.0; consistent with the same
  change landing across grcache/grevents/graudit/grnoti in this pass.
  Real structured field values (previously flattened into printf format
  strings) now reach any structured-output logger intact.

### Changed

- Folded the standalone `conformance` package into the root package as
  `contract_engine_test.go` (`runEngineContract`, run via
  `TestEngine_Contract`), matching the rest of the gourdian ecosystem's
  convention of keeping shared behavioral suites as root-package test files
  rather than a separately importable package. The vestigial, never-used
  `RunOption`/`runConfig` extension point was dropped in the same pass. See
  `docs/architecture.md` §6.
- `make coverage-check`'s threshold raised from 80% to 95%.

### Fixed

- `Evaluate`'s array-literal and function-call argument evaluation
  (`evalArray`, `evalArgs` in `eval.go`) now stub every not-yet-reached
  element/argument in `Decision.Trace.Children` when an earlier one fails,
  mirroring the convention every binary operator (`&&`, `==`, `contains`,
  `matches`, `in`, ...) already followed for its own unevaluated sibling.
  Previously, `Children` for an array or call expression was silently
  shorter than the expression's actual element/argument count whenever an
  error occurred partway through — a real inconsistency for any caller
  relying on `Trace` mirroring the compiled expression's full static shape.
- `Compile`'s (and `Evaluate`'s own defensive duplicate check's) rejection
  of a non-identifier call target (e.g. `a.b()`, member/method-call syntax
  grpolicy doesn't support) now wraps `ErrCompileFailed`. Previously this
  error wrapped no sentinel at all, so `errors.Is(err, ErrCompileFailed)`
  returned false for it — inconsistent with every other `Compile` failure
  mode, and undocumented on `Compile`'s own doc comment.

### Testing

- Coverage raised to 99.8% on the root package (previously 88.7%), closing
  nearly every gap: unimplemented binary/unary operators only caught at
  Evaluate time (grpolicy validates function calls at Compile time but not
  arbitrary operators), `&&`/`||`/comparison/string-op/matches erroring on
  either operand, the two new array/call stub branches above, a reserved
  builtin name grpolicy doesn't implement (`len`, `all`), nested
  unknown-function errors, function-registry arity/type/conversion edge
  cases (variadic minimum-argument checks, nil-argument zero-valuing,
  numeric-vs-non-numeric conversion rejection, a single-`error`-return
  function), grpolicy's own hardcoded `before`/`after`/`addDuration`
  built-ins (registered but never previously invoked by any test), and a
  handful of purely defensive branches unreachable from the public API
  alone — covered in a new `internal_coverage_test.go` (white-box,
  `package grpolicy`) by constructing AST nodes or calling unexported
  helpers directly, the same technique used elsewhere in the gourdian
  ecosystem. Two gaps remain and are accepted as permanent: `cache.go`'s
  `back == nil` guard in `policyCache.put`'s eviction loop (structurally
  unreachable — the loop's own condition guarantees a non-empty list) and
  `noopLogger`'s three single-line no-op methods (a Go tooling artifact for
  completely empty function bodies — they contribute 0 total statements
  each, so the aggregate percentage `coverage-check` gates on is
  unaffected).

### Documentation

- README: added a "Part of the gourdian25 ecosystem" section (naming every
  sibling repo plus the not-yet-built `grauth`) and a Table of Contents,
  matching the format already used by `grlog`/`grevents`.
- README: added a brief Contributing section (previously none) pointing at
  `make lint`, `make test`, and `make race`, and stated the actual verified
  test-coverage figure (99.8% on the root package, `make coverage-check`,
  2026-07-22) near the Testing section.

## [0.1.1] - 2026-07-10

### Fixed

- Removed a stray, empty `LICENCE` (British spelling) that duplicated the
  real `LICENSE`, and a stray, empty `CHANGELOG.md.gitignore` — both
  leftover artifacts from the initial commit being copy-pasted from
  graudit's template.

### Changed

- Bumped `github.com/gourdian25/grlog` to `v0.1.1`.

## [0.1.0] - 2026-07-10

Initial release. grpolicy is a standalone attribute-based policy/expression
evaluation engine for the gourdian ecosystem — not authentication, not
authorization, no concept of "user"/"role"/"permission." Given a compiled
boolean expression and a `map[string]any` of attributes, `Evaluate` returns
a `Decision{Allowed, Trace}`. It is the future `grauth` repo's primary
dependency.

### Added

- **Expression language**: boolean logic (`&&`, `||`, `!`/`not`),
  comparisons (`==`, `!=`, `<`, `<=`, `>`, `>=`), string operators
  (`contains`, `startsWith`, `endsWith`, regex `matches`), and membership
  (`in`) against slices, arrays, and map keys.
- **Parsing via `github.com/expr-lang/expr`'s `parser.Parse` only** — never
  that library's own `Run`/`vm.Program` execution path. grpolicy writes its
  own tree-walking evaluator (`eval.go`) directly over the returned AST, so
  `Decision.Trace` is a byproduct of the same walk that produces the
  result, not a second, potentially-divergent reimplementation. See
  `docs/architecture.md` for the full reasoning, including why
  `google/cel-go` was considered and rejected.
- **`now()` and `duration()` built-ins**, implemented directly as
  `*ast.BuiltinNode` handlers (a hard constraint of expr-lang/expr's
  parser, which reserves these identifiers — see `docs/architecture.md`
  §9), plus `before`/`after`/`addDuration` as ordinary registered
  functions.
- **`RegisterFunction`** for custom functions, reached via ordinary
  `*ast.CallNode` dispatch through a reflect-based registry with arity/type
  checking and panic recovery. Registration of any name reserved by
  expr-lang/expr's own grammar is rejected outright — such a name could
  never be reached via `CallNode` dispatch regardless.
- **Explainable `Decision.Trace`**: the full expression shape as written,
  including `&&`/`||` branches that were short-circuited and never
  actually ran (marked `Evaluated: false`, `Result: nil` — never a coerced
  `false`).
- **Fail-loud semantics**: a missing attribute key (`ErrAttributeMissing`)
  or an operator applied to incompatible types (`ErrTypeMismatch`) is
  always an error, never a silently substituted zero value or coerced
  comparison — with the partial `Trace` built up to the failure point
  still returned alongside the error.
- **Compile-time complexity limits** (`WithMaxComplexity`, default 1000
  nodes / depth 100): a pathologically large or deep expression is
  rejected at `Compile`, never reaching `Evaluate`.
- **`ctx`-respecting evaluation**: checked before every sub-expression
  evaluation, not just once at the start of `Evaluate` — proven by a
  timeout test combining a slow custom function with a subsequent
  operation that must never run.
- **A bounded, concurrency-safe compiled-policy cache** keyed by the raw,
  unnormalized source string (default capacity 512, configurable via
  `WithCacheSize`; `WithUnboundedCache` opts in explicitly to no limit).
  Verified safe under real concurrent `Compile`/`Evaluate` load via a
  dedicated `-race` stress test, not assumed from a static read of the
  locking code.
- **RE2-only regex** (`matches`): Go's standard-library `regexp` package is
  the only regex engine used anywhere in the module, confirmed ReDoS-safe
  against a classically-pathological pattern, with the `ctx` deadline as a
  documented second line of defense even if a pattern were ever slow.
- **Sentinel errors** (`ErrCompileFailed`, `ErrTooComplex`,
  `ErrUnknownFunction`, `ErrEvaluationTimeout`, `ErrTypeMismatch`,
  `ErrAttributeMissing`, `ErrClosed`), `errors.Is`-compatible, no `IsX(err)
  bool` helpers — matching the ecosystem-wide convention.
- **Optional `Logger`** (`Infof`/`Warnf`/`Errorf`) — the same three-method
  shape as every sibling repo; grpolicy itself never imports grlog.
- **`FuzzCompileEvaluate`**: fuzzes `Compile`+`Evaluate` against arbitrary
  and malformed expression text — 40M+ executions across development with
  zero panics or hangs found. The first sibling repo where fuzzing is
  actually relevant, given grpolicy's tree-walking evaluator is entirely
  new code with no upstream test coverage from expr-lang/expr itself.
- `conformance/` — grpolicy's own primary behavioral suite (not a
  backend-parity suite, since there's only one implementation; see
  `docs/architecture.md` §6 for this deliberate reshaping of the sibling
  pattern), covering every operator, the short-circuit trace guarantee,
  fail-loud semantics, complexity limits, timeouts, function-registry edge
  cases (reserved names, double registration, panic recovery), a full
  type-coercion matrix, and the concurrent cache-stress scenario.
- `example/example.go` — a runnable demo requiring no external services,
  printing `Decision.Trace` for both an allowed and a short-circuited
  denied decision.
