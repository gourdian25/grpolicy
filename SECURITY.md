# Security Policy

## Supported Versions

Security fixes are applied to the latest released minor version.

| Version | Supported |
|---------|-----------|
| 0.3.x   | ✅        |
| < 0.3   | ❌        |

## Reporting a Vulnerability

Please report suspected vulnerabilities privately via
[GitHub Security Advisories](https://github.com/gourdian25/grpolicy/security/advisories/new)
rather than opening a public issue.

Include:

- A description of the issue and its impact
- Steps or a proof-of-concept to reproduce
- Affected version(s)

You can expect an acknowledgment within a week. Once a fix is available, the
advisory will be published together with a patched release.

## Scope Notes

grpolicy is a policy/expression evaluation engine intended to gate real
access and business-rule decisions in the calling application. Read this
before relying on it for anything security-relevant:

- **No code execution beyond registered functions.** grpolicy never calls
  `expr-lang/expr`'s own `Run`/`vm.Program` execution path — only
  `parser.Parse`, to obtain an AST. The custom tree-walking evaluator
  (`eval.go`) only ever invokes: the fixed operator set (`&&`, `||`, `!`,
  comparisons, string operators, `in`), the two built-ins it implements
  directly (`now()`, `duration()`), and functions explicitly registered via
  `RegisterFunction`. A policy string cannot invoke arbitrary Go code, read
  files, make network calls, or otherwise escape this fixed surface —
  **except through whatever a registered function's own body does**, which
  is entirely the registering caller's responsibility, not grpolicy's. If
  you register a function that reads a file or makes a network call, a
  policy string that reaches it can do the same.
- **ReDoS protection for `matches`.** Only Go's standard-library `regexp`
  package (RE2-based, linear-time by construction) is used anywhere in the
  module — confirmed by a test exercising a classically-catastrophic
  pattern against adversarial input, not merely by inspecting imports.
  `ctx` cancellation is a documented second line of defense even in the
  hypothetical case a pattern were ever slow: `Evaluate` checks `ctx.Done()`
  before every sub-expression, including before a `matches` call.
- **Expression complexity limits.** `Compile` rejects a pathologically
  large or deeply nested expression (`ErrTooComplex`, default 1000 AST
  nodes / depth 100, configurable via `WithMaxComplexity`) before it can
  ever reach `Evaluate` — an adversarially large policy string cannot
  cause unbounded parse/evaluation cost.
- **`ctx` deadlines are checked between sub-expressions, not enforced
  mid-call.** A registered custom function that blocks inside its own body
  (e.g. an unbounded loop, a slow network call with no timeout) is not
  preemptible by grpolicy — `Evaluate`'s `ctx` check runs before invoking
  the function and again before the *next* one, but cannot interrupt an
  in-flight `reflect.Value.Call`. If a registered function's own runtime
  needs to be bounded, it must accept and honor its own `context.Context`
  (or an internal timeout) — grpolicy's `ctx` alone does not backstop a
  function that ignores cancellation internally.
- **Reflection-based struct field access is read-only and export-scoped.**
  Nested attribute access (`resource.OwnerID`, `docs/architecture.md` §12)
  against a Go struct value can read any *exported* field reachable from it —
  including through embedded/promoted fields — and nothing else: it never
  invokes a method, never writes a field, and an unexported field is
  reported exactly like a nonexistent one (`ErrAttributeMissing`), never
  exposed even by probing for the difference. Any unexpected panic from the
  underlying `reflect` operations (guarded against wherever possible; a
  backstop for what those guards miss) is recovered into an error, the same
  posture as a panic inside a registered custom function — it is a bug in
  the caller's data shape or in grpolicy itself, never a way to crash the
  process.
- **No policy storage, no policy provenance.** grpolicy never persists,
  signs, or authenticates the expression string it's given — where it came
  from (a trusted config file vs. an untrusted multi-tenant input) is
  entirely the caller's problem. If policy strings may originate from a
  less-trusted actor (e.g. a multi-tenant application where tenant admins
  author their own ABAC rules), the caller is responsible for any
  authorization/review of the policy text itself before calling `Compile`.
- **`Decision.Trace` is a debugging/explainability aid, not an audit
  record.** It is constructed fresh on every `Evaluate` call and is not
  persisted, signed, or tamper-evident. Pair grpolicy with
  [graudit](https://github.com/gourdian25/graudit) if you need a durable,
  tamper-evident record of *why* a decision was made.
