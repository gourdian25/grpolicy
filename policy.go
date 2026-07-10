// File: policy.go

package grpolicy

import (
	"context"
	"regexp"
	"sync"

	"github.com/expr-lang/expr/ast"
)

// CompiledPolicy is a parsed, validated, reusable expression returned by
// Engine.Compile. A single CompiledPolicy is safe for concurrent use by
// multiple goroutines calling Evaluate.
type CompiledPolicy interface {
	// Evaluate runs the compiled policy against attrs. Respects ctx
	// cancellation/deadline — ctx.Done() is checked before evaluating each
	// sub-expression, not just once at the start, so a slow custom function
	// or a deeply nested expression cannot hang an Evaluate call indefinitely
	// once the deadline has passed the next check point.
	//
	// Parameters:
	//   - ctx: context.Context — checked between sub-expression evaluations
	//   - attrs: map[string]any — the attribute values the expression is
	//     evaluated against; a key referenced by the expression but absent
	//     from attrs fails with ErrAttributeMissing rather than being
	//     silently treated as a zero value
	//
	// Returns:
	//   - Decision: Allowed is only meaningful when error is nil. Trace is
	//     always populated with whatever of the expression's tree was built
	//     before any error occurred — see Decision's doc comment for the
	//     precise guarantee.
	//   - error: ErrAttributeMissing, ErrTypeMismatch, ErrEvaluationTimeout,
	//     or an error from a registered custom function
	//
	// Example:
	//
	//	decision, err := policy.Evaluate(ctx, map[string]any{"age": 21, "country": "US"})
	//	if err != nil {
	//		// decision.Trace still shows how far evaluation got
	//		return err
	//	}
	//	if decision.Allowed { ... }
	Evaluate(ctx context.Context, attrs map[string]any) (Decision, error)

	// Source returns the original expression text this was compiled from.
	Source() string
}

// Decision is the result of evaluating a CompiledPolicy against a set of
// attributes.
//
// The precise claim (read this before trusting Trace for anything beyond
// debugging/explainability): Trace reflects the full static shape of the
// compiled expression's tree as it existed at Compile time — every &&/||
// branch appears in Trace even if it was short-circuited and never actually
// evaluated (see CheckNode.Evaluated). This means Trace shows what the
// policy author wrote, not only what happened to run on this particular
// call. On a non-nil error from Evaluate, Trace contains every CheckNode
// successfully built up to the point of failure; Allowed is meaningless
// (always false) in that case and must never be treated as a real policy
// decision — check the error first.
type Decision struct {
	// Allowed is the boolean result of the policy. Only meaningful when
	// Evaluate returned a nil error.
	Allowed bool

	// Trace is the root of the expression's check tree. It contains exactly
	// one element (the root node) on success or on partial failure; it is
	// empty only if evaluation failed before the root node's own children
	// could be constructed at all (never for a non-empty expression that
	// reached at least one identifier or literal).
	Trace []CheckNode
}

// CheckNode is one node of a Decision.Trace — either an operator applied to
// its Children, or a leaf (identifier, literal, or function call) with no
// children.
type CheckNode struct {
	// Expression is the sub-expression's source text, sliced from the
	// original compiled expression.
	Expression string

	// Operator is the operator this node represents (e.g. "&&", "==", "in"),
	// or "" for a leaf node (identifier, literal, or function call result).
	Operator string

	// Result is the value this sub-expression evaluated to. Result is left
	// nil whenever Evaluated is false — a node with Evaluated == true and
	// Result == false is a node that actually ran and returned false; these
	// two states are never conflated. A nil Result with Evaluated == true is
	// possible only for a node whose value is legitimately nil.
	Result any

	// Passed is Result coerced to boolean truthiness. Only meaningful when
	// Evaluated is true.
	Passed bool

	// Children are this node's sub-expressions, in source order. Empty for
	// leaf nodes.
	Children []CheckNode

	// Evaluated is false if this node was short-circuited by a parent &&/||
	// and never actually ran — see the package-level short-circuit policy
	// documented on Decision. It is also false for any node at or beyond the
	// point where an error (ErrAttributeMissing, ErrTypeMismatch, a custom
	// function error) stopped evaluation.
	Evaluated bool
}

// compiledPolicy is the concrete CompiledPolicy implementation returned by
// engine.Compile.
type compiledPolicy struct {
	source string
	root   ast.Node
	engine *engine

	// nodeCount and depth are computed once at Compile time by
	// measureComplexity, so repeated Evaluate calls never re-walk the tree
	// just to re-check a limit that was already enforced at Compile time.
	nodeCount int
	depth     int

	// regexCache holds one compiled *regexp.Regexp per distinct pattern
	// string used by a "matches" operator in this policy, built lazily on
	// first use and reused by every subsequent Evaluate call so repeated
	// evaluation never recompiles the same pattern.
	regexMu    sync.Mutex
	regexCache map[string]*regexp.Regexp
}

func (p *compiledPolicy) Source() string { return p.source }

func (p *compiledPolicy) compiledRegex(pattern string) (*regexp.Regexp, error) {
	p.regexMu.Lock()
	defer p.regexMu.Unlock()
	if re, ok := p.regexCache[pattern]; ok {
		return re, nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	p.regexCache[pattern] = re
	return re, nil
}
