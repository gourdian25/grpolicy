# grpolicy

[![Go Reference](https://pkg.go.dev/badge/github.com/gourdian25/grpolicy.svg)](https://pkg.go.dev/github.com/gourdian25/grpolicy)
[![Go Version](https://img.shields.io/badge/go-1.26+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-green)](LICENSE)

A standalone attribute-based policy/expression evaluation engine for the
gourdian ecosystem.

grpolicy is **not** authentication and **not** authorization — it has no
concept of "user," "role," or "permission." Given a compiled boolean
expression and a `map[string]any` of attributes, it returns a boolean
decision plus an explainable trace of exactly how that decision was
reached. [grauth](https://github.com/gourdian25/grauth) (not yet built)
will own the vocabulary of "who has which role"; grpolicy only ever sees
attribute maps, and every RBAC/ABAC decision grauth makes is meant to route
through `grpolicy.Evaluate`.

## Part of the gourdian25 ecosystem

grpolicy is one of several small, independent Go libraries meant to be used
together:

- [gourdiantoken](https://github.com/gourdian25/gourdiantoken) — JWT
  access/refresh token issuance, verification, revocation, and rotation.
- [grlog](https://github.com/gourdian25/grlog) — zero-dependency structured
  logging; grpolicy's optional `Logger` interface is satisfied by it
  directly.
- [grcache](https://github.com/gourdian25/grcache) — backend-agnostic
  caching abstraction (Redis, Postgres, Mongo, memcached, in-memory).
- [grevents](https://github.com/gourdian25/grevents) — an in-process event
  bus for decoupling producers of state changes from consumers that react
  to them.
- [graudit](https://github.com/gourdian25/graudit) — an append-only,
  tamper-evident audit log with pluggable storage backends.
- [grnoti](https://github.com/gourdian25/grnoti) — a push-notification
  service (FCM dispatch, idempotent event processing, device-token
  management, dead-letter-queue retry, circuit breaking, distributed rate
  limiting, deterministic A/B experiment assignment, localization, and
  topic-based routing); grpolicy has no dependency on it — this is only a
  sibling in the ecosystem, since grpolicy has no storage backend of its
  own to integrate with.
- [grauth](https://github.com/gourdian25/grauth) (not yet built) — the
  ecosystem's authentication/authorization layer and grpolicy's primary
  consumer; every RBAC/ABAC decision it makes is meant to route through
  `grpolicy.Evaluate`.

## Table of Contents

- [The precise claim](#the-precise-claim-read-this-before-trusting-decisiontrace)
- [Install](#install)
- [Quickstart](#quickstart)
- [Expression language](#expression-language)
- [Custom functions](#custom-functions)
- [Error handling](#error-handling)
- [Complexity limits and timeouts](#complexity-limits-and-timeouts)
- [Compiled-policy caching](#compiled-policy-caching)
- [Logger](#logger)
- [Testing](#testing)
- [Out of scope (not currently supported)](#out-of-scope-not-currently-supported)
- [Contributing](#contributing)
- [License](#license)

## The precise claim (read this before trusting `Decision.Trace`)

`Decision.Trace` reflects the full static shape of the compiled expression
as written — every `&&`/`||` branch appears in the trace even if it was
short-circuited and never actually evaluated (`CheckNode.Evaluated ==
false`, `Result == nil`, never a coerced `false`). This means the trace
shows what the policy author wrote, not only what happened to run on a
particular call — useful for understanding a policy's full shape while
debugging a `DENY`, but don't assume every `CheckNode` in it corresponds to
a function that actually executed.

The trace is a byproduct of grpolicy's **own** tree-walking evaluator
(`eval.go`), not of `expr-lang/expr`'s `Run`/VM path — grpolicy uses that
library for parsing only (`parser.Parse`, to obtain an AST) and never
touches its execution engine. This matters because it means the trace can
never diverge from what actually gated the decision — there is only one
evaluation semantics in play, not two.

A missing attribute key or a type mismatch (e.g. comparing a string
attribute against a numeric literal) always returns a non-nil error
(`ErrAttributeMissing` / `ErrTypeMismatch`) — never a silently substituted
zero value or coerced comparison. `Decision.Trace` is still returned
alongside that error, populated with whatever was successfully evaluated
up to the point of failure; `Decision.Allowed` is meaningless in that case
and must not be treated as a real policy decision. Check the error first.

`Decision.Trace` is built fresh on every `Evaluate` call — it is not
persisted, signed, or tamper-evident. See
[SECURITY.md](SECURITY.md) for the full security scope, including ReDoS
protection, complexity limits, and exactly what a registered custom
function can and can't do.

## Install

```sh
go get github.com/gourdian25/grpolicy
```

## Quickstart

```go
import (
	"context"
	"log"

	"github.com/gourdian25/grpolicy"
)

func main() {
	engine, err := grpolicy.NewEngine()
	if err != nil {
		log.Fatal(err)
	}
	defer engine.Close()

	policy, err := engine.Compile(`age >= 18 && country in allowedCountries`)
	if err != nil {
		log.Fatal(err)
	}

	decision, err := policy.Evaluate(context.Background(), map[string]any{
		"age":              21,
		"country":          "US",
		"allowedCountries": []string{"US", "CA"},
	})
	if err != nil {
		log.Fatal(err)
	}
	log.Println(decision.Allowed) // true
}
```

See [example/example.go](example/example.go) for a fuller runnable demo,
including `RegisterFunction` and a printed `Decision.Trace` for both an
allowed and a short-circuited denied decision.

## Expression language

- **Boolean logic**: `&&`, `||`, `!` / `not`
- **Comparisons**: `==`, `!=`, `<`, `<=`, `>`, `>=` — numeric types
  (`int`/`int8`.../`float64`) are normalized against each other; `string`
  vs `string` compares lexicographically; `time.Time` vs `time.Time`
  compares chronologically. Comparing across categories (e.g. a numeric
  attribute against a string) is always `ErrTypeMismatch`, never coerced.
- **String operators**: `contains`, `startsWith`, `endsWith`, regex
  `matches` (both operands must be strings)
- **Membership**: `in`, against a slice/array (any element type) or a
  `map[string]any`'s keys
- **Built-ins**: `now()`, `duration("1h")`, plus `before`/`after`/
  `addDuration` for `time.Time` values

Attributes are a flat `map[string]any` — nested paths like
`user.address.city` are a v0.2+ candidate, not currently supported.

## Custom functions

```go
if err := engine.RegisterFunction("isBusinessHours", func(hour int) bool {
	return hour >= 9 && hour < 17
}); err != nil {
	log.Fatal(err)
}

policy, err := engine.Compile(`isBusinessHours(hour)`)
```

Register before compiling any policy that references the function name —
`Compile` validates every function reference up front, so an unregistered
name fails at `Compile` with `ErrUnknownFunction`, never silently deferred
to `Evaluate`. A function may return `(T)` or `(T, error)`; a panic inside
a registered function is recovered and surfaced as an error, never crashes
the process.

Names reserved by expr-lang/expr's own grammar (`now`, `duration`, `len`,
`all`, and others — see `github.com/expr-lang/expr/builtin.Index`) cannot
be registered: a call to one of those names always parses to the parser's
own reserved meaning, never to a registered function, so registering one
would be silently unreachable — `RegisterFunction` rejects this outright
rather than accepting a registration that could never fire.

## Error handling

```go
decision, err := policy.Evaluate(ctx, attrs)
if errors.Is(err, grpolicy.ErrAttributeMissing) {
	// a referenced attribute key wasn't in the map
} else if errors.Is(err, grpolicy.ErrTypeMismatch) {
	// an operator was applied to incompatible types
} else if err != nil {
	// ErrEvaluationTimeout, or an error from a registered function
}
```

Sentinel errors (`ErrCompileFailed`, `ErrTooComplex`, `ErrUnknownFunction`,
`ErrEvaluationTimeout`, `ErrTypeMismatch`, `ErrAttributeMissing`,
`ErrClosed`) are for use with `errors.Is` — there is deliberately no
`IsX(err) bool` helper for any of them.

## Complexity limits and timeouts

```go
engine, err := grpolicy.NewEngine(
	grpolicy.WithMaxComplexity(1000, 100), // AST node count, nesting depth — these are the defaults
)
```

A pathologically large or deeply nested expression is rejected at
`Compile` (`ErrTooComplex`), never reaching `Evaluate`. `Evaluate` checks
`ctx.Done()` before every sub-expression, not just once at the start — a
slow custom function or a deeply nested expression can be interrupted
between any two sub-expressions. See [SECURITY.md](SECURITY.md) for the
precise limits of what `ctx` can and can't preempt mid-call.

## Compiled-policy caching

`Compile` caches by the exact, unnormalized source string — `"a && b"` and
`"a&&b"` are semantically identical but hit two separate cache entries;
calling `Compile` with byte-identical text is cheap. Default capacity is
512 distinct policies (`WithCacheSize`); `WithUnboundedCache` opts in
explicitly to no limit. See `docs/architecture.md` §10 for why raw-string
keying was chosen over AST-based normalization.

## Logger

```go
import "github.com/gourdian25/grlog"

logger := grlog.NewDefaultLogger()
engine, err := grpolicy.NewEngine(grpolicy.WithLogger(logger)) // *grlog.Logger satisfies grpolicy.Logger directly
```

grpolicy accepts any logger satisfying its own small `Logger` interface
(`Infof`/`Warnf`/`Errorf`) — the same three-method shape every sibling repo
uses. grpolicy itself never imports grlog; a `nil` Logger (the default)
means grpolicy logs nothing.

## Testing

```sh
make test             # go test -cover ./...
make race             # go test -race ./...  (mandatory before any commit touching eval.go or cache.go)
make coverage-check   # verify the root package meets 95% coverage
make fuzz             # fuzz Compile+Evaluate for 3m (FUZZTIME=... to override)
make bench             # go test -bench=. -benchmem -benchtime=10s ./...
make lint              # golangci-lint run ./...
```

`contract_engine_test.go` is grpolicy's own primary behavioral suite (not a
backend-parity suite — there's only one implementation; see
`docs/architecture.md` §6), covering every operator, the short-circuit
trace guarantee, fail-loud semantics, complexity limits, timeouts,
function-registry edge cases, a full type-coercion matrix, and a
concurrent `-race` cache-stress scenario. `FuzzCompileEvaluate` fuzzes
`Compile`+`Evaluate` against arbitrary and malformed expression text — no
live services required for any of grpolicy's own tests, since it's a pure
computation engine with no storage backend.

`make bench` runs `go test -bench=.` as advertised, but there are currently
no `func Benchmark*` functions anywhere in this repo — the command will
build and run the test suite but report no benchmark results. The real
testing surface is the behavioral suite above, `-race` (including the
concurrent cache-stress scenario), and `FuzzCompileEvaluate`; microbenchmarks
may be added later if a hot path needs one.

Current root-package coverage is **99.8%**, verified via `make
coverage-check` on 2026-07-22 — comfortably above the 95% gate that command
enforces.

## Out of scope (not currently supported)

Any RBAC/ABAC data modeling — grpolicy has no "role" or "permission" as a
first-class type, only attribute maps; this boundary is deliberate (see
`docs/plan/grpolicy-plan.md`). Policy storage — grpolicy never persists
the expression string anywhere; where it comes from is entirely the
caller's problem. Nested/hierarchical attribute paths
(`user.address.city`) — a v0.2+ candidate. A policy authoring UI or CLI
validator tool.

## Contributing

Issues and PRs are welcome at
[github.com/gourdian25/grpolicy](https://github.com/gourdian25/grpolicy).
Please run `make lint`, `make test`, and `make race` before submitting (see
Testing above for what each covers).

## License

MIT — see [LICENSE](LICENSE).
