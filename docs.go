// File: docs.go

// Package grpolicy provides a standalone attribute-based policy/expression
// evaluation engine for the gourdian ecosystem.
//
// Overview:
//
// grpolicy is not authentication and not authorization — it knows nothing
// about "users," "roles," or "permissions." Given a compiled boolean
// expression and a map[string]any of attributes, it returns a Decision
// (Allowed plus an explainable Trace of exactly how that result was
// reached). It is the future grauth repo's primary dependency: every
// RBAC/ABAC decision grauth makes will route through grpolicy.Evaluate,
// with grauth owning the vocabulary of "who has which role" and grpolicy
// only ever seeing attribute maps.
//
// Key Features:
//   - Boolean logic (&&, ||, !/not), comparisons (==, !=, <, <=, >, >=),
//     string operators (contains, startsWith, endsWith, regex matches), and
//     membership (in) against slices, arrays, and map keys
//   - now() and duration() built-ins, plus custom function registration via
//     RegisterFunction for anything else (e.g. a future
//     isBusinessHours())
//   - Compiled-policy caching: Compile results are cached by source string,
//     so repeated compilation of the same policy text is cheap
//   - An explainable Decision.Trace — including short-circuited &&/||
//     branches, marked Evaluated: false — see Decision's doc comment for
//     the precise claim
//   - Expression complexity limits (max AST node count / nesting depth)
//     enforced at Compile time
//   - Evaluation respects ctx cancellation/deadline between sub-expression
//     evaluations, not just once at the start of Evaluate
//   - Sentinel errors (ErrCompileFailed, ErrTooComplex, ErrUnknownFunction,
//     ErrEvaluationTimeout, ErrTypeMismatch, ErrAttributeMissing,
//     ErrClosed) usable with errors.Is
//   - Optional diagnostic logging via any logger satisfying the small
//     Logger interface — grlog.Logger works with no adapter
//
// Getting Started:
//
//	import (
//		"context"
//		"log"
//
//		"github.com/gourdian25/grpolicy"
//	)
//
//	func main() {
//		engine, err := grpolicy.NewEngine()
//		if err != nil {
//			log.Fatal(err)
//		}
//		defer engine.Close()
//
//		policy, err := engine.Compile(`age >= 18 && country in allowedCountries`)
//		if err != nil {
//			log.Fatal(err)
//		}
//
//		decision, err := policy.Evaluate(context.Background(), map[string]any{
//			"age":               21,
//			"country":           "US",
//			"allowedCountries":  []string{"US", "CA"},
//		})
//		if err != nil {
//			log.Fatal(err)
//		}
//		log.Println(decision.Allowed) // true
//	}
//
// Architecture:
//
// grpolicy uses github.com/expr-lang/expr for exactly one thing: parsing
// source text into an AST (parser.Parse). It never calls that library's own
// expr.Compile/expr.Run/vm.Program execution path — grpolicy writes its own
// tree-walking evaluator (eval.go) that walks the parsed AST directly,
// building a CheckNode at every node as it goes. This is what makes
// Decision.Trace possible without a second, potentially divergent
// reimplementation of evaluation semantics: the trace is a byproduct of the
// same walk that produces the result, not derived after the fact. See
// docs/architecture.md for the full reasoning, including why expr-lang/expr
// was chosen over google/cel-go, and a real constraint discovered while
// building the evaluator: expr-lang/expr's parser reserves certain bare
// identifiers (e.g. "now", "duration") for its own builtin grammar, which
// affects how grpolicy implements those two built-ins and what names
// RegisterFunction will reject.
//
// A Compile call is cached by source string in a bounded, LRU,
// mutex-protected cache (cache.go) — Compile with identical text repeatedly
// is cheap, and its correctness under real concurrent Compile/Evaluate
// traffic is verified by a dedicated -race stress test, not assumed from a
// static read of the locking code.
//
// Testing:
//
// grpolicy's behavioral suite lives in the root package's own
// contract_engine_test.go, run via TestEngine_Contract. -race is mandatory
// when running the suite — the ConcurrentCompileEvaluateStress scenario is
// meaningless without it.
//
// Out of Scope:
//
// grpolicy has no concept of "role" or "permission" as first-class types —
// only attribute maps; if implementation pressure from a future grauth
// design starts pulling role/permission vocabulary in here, that is a sign
// the boundary is wrong, not a reason to add it. grpolicy never persists
// policy text anywhere — where the expression string comes from (a config
// file, a database row, a hardcoded string) is entirely the caller's
// problem. Attribute paths are flat (map[string]any) in v0.1.0; nested
// access like user.address.city is a v0.2+ candidate. There is no policy
// authoring UI or CLI validator tool.
//
// License: MIT
// Repository: https://github.com/gourdian25/grpolicy
package grpolicy
