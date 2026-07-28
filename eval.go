// File: eval.go

package grpolicy

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"strings"
	"time"

	"github.com/expr-lang/expr/ast"
)

// Evaluate runs p against attrs. See the CompiledPolicy interface doc
// comment for the full contract; the summary here is mechanical: eval
// builds the Decision.Trace inline as it walks the AST, and returns it
// (however far it got) alongside any error, per the package's precise-claim
// documentation on Decision.
func (p *compiledPolicy) Evaluate(ctx context.Context, attrs map[string]any) (Decision, error) {
	w := &walker{ctx: ctx, attrs: attrs, fns: p.engine.functions, policy: p, source: p.source}
	result, root, err := w.eval(p.root)

	decision := Decision{Trace: []CheckNode{root}}
	if err != nil {
		return decision, err
	}
	decision.Allowed = truthy(result)
	return decision, nil
}

// walker carries the per-Evaluate-call state threaded through every
// recursive eval call, so eval itself only needs to take the node being
// evaluated.
type walker struct {
	ctx    context.Context
	attrs  map[string]any
	fns    *functionRegistry
	policy *compiledPolicy
	source string
}

// eval evaluates n, returning its value, the CheckNode describing it (always
// populated, even on error — see the partial-trace guarantee on Decision),
// and an error if evaluation failed. ctx is checked before every single call
// to eval, not just once per Evaluate, so a deeply nested or slow expression
// is interruptible between any two sub-expressions.
func (w *walker) eval(n ast.Node) (any, CheckNode, error) {
	select {
	case <-w.ctx.Done():
		return nil, CheckNode{Expression: exprText(n, w.source), Operator: operatorOf(n)}, fmt.Errorf("grpolicy: %w", ErrEvaluationTimeout)
	default:
	}

	switch node := n.(type) {
	case *ast.BinaryNode:
		return w.evalBinary(node)
	case *ast.UnaryNode:
		return w.evalUnary(node)
	case *ast.IdentifierNode:
		return w.evalIdentifier(node)
	case *ast.CallNode:
		return w.evalCall(node)
	case *ast.BuiltinNode:
		return w.evalBuiltin(node)
	case *ast.ArrayNode:
		return w.evalArray(node)
	case *ast.MemberNode:
		return w.evalMember(node)
	case *ast.ChainNode:
		return w.evalChain(node)
	case *ast.StringNode:
		return w.leaf(node, node.Value)
	case *ast.IntegerNode:
		return w.leaf(node, node.Value)
	case *ast.FloatNode:
		return w.leaf(node, node.Value)
	case *ast.BoolNode:
		return w.leaf(node, node.Value)
	case *ast.NilNode:
		return w.leaf(node, nil)
	default:
		return nil, CheckNode{Expression: exprText(n, w.source)}, fmt.Errorf("grpolicy: unsupported expression node %T", n)
	}
}

func (w *walker) leaf(n ast.Node, value any) (any, CheckNode, error) {
	return value, CheckNode{
		Expression: exprText(n, w.source),
		Result:     value,
		Passed:     truthy(value),
		Evaluated:  true,
	}, nil
}

// buildStub builds the CheckNode shape for n and every descendant without
// evaluating anything — no attribute lookup, no function call is ever
// invoked. This is what makes a short-circuited &&/|| branch provably
// unevaluated: it never reaches eval at all. See children() for the exact
// node-kind traversal this mirrors.
func buildStub(n ast.Node, source string) CheckNode {
	node := CheckNode{
		Expression: exprText(n, source),
		Operator:   operatorOf(n),
		Evaluated:  false,
	}
	for _, c := range children(n) {
		node.Children = append(node.Children, buildStub(c, source))
	}
	return node
}

func (w *walker) evalBinary(node *ast.BinaryNode) (any, CheckNode, error) {
	switch node.Operator {
	case "&&":
		return w.evalAnd(node)
	case "||":
		return w.evalOr(node)
	case "==", "!=", "<", "<=", ">", ">=":
		return w.evalComparison(node)
	case "contains", "startsWith", "endsWith":
		return w.evalStringOp(node)
	case "matches":
		return w.evalMatches(node)
	case "in":
		return w.evalIn(node)
	default:
		return nil, CheckNode{Expression: exprText(node, w.source), Operator: node.Operator}, fmt.Errorf("grpolicy: unsupported operator %q", node.Operator)
	}
}

func (w *walker) evalAnd(node *ast.BinaryNode) (any, CheckNode, error) {
	check := CheckNode{Expression: exprText(node, w.source), Operator: "&&"}

	leftVal, leftCheck, err := w.eval(node.Left)
	if err != nil {
		check.Children = []CheckNode{leftCheck, buildStub(node.Right, w.source)}
		return nil, check, err
	}

	if !truthy(leftVal) {
		check.Children = []CheckNode{leftCheck, buildStub(node.Right, w.source)}
		check.Result, check.Passed, check.Evaluated = false, false, true
		return false, check, nil
	}

	rightVal, rightCheck, err := w.eval(node.Right)
	check.Children = []CheckNode{leftCheck, rightCheck}
	if err != nil {
		return nil, check, err
	}
	result := truthy(rightVal)
	check.Result, check.Passed, check.Evaluated = result, result, true
	return result, check, nil
}

func (w *walker) evalOr(node *ast.BinaryNode) (any, CheckNode, error) {
	check := CheckNode{Expression: exprText(node, w.source), Operator: "||"}

	leftVal, leftCheck, err := w.eval(node.Left)
	if err != nil {
		check.Children = []CheckNode{leftCheck, buildStub(node.Right, w.source)}
		return nil, check, err
	}

	if truthy(leftVal) {
		check.Children = []CheckNode{leftCheck, buildStub(node.Right, w.source)}
		check.Result, check.Passed, check.Evaluated = true, true, true
		return true, check, nil
	}

	rightVal, rightCheck, err := w.eval(node.Right)
	check.Children = []CheckNode{leftCheck, rightCheck}
	if err != nil {
		return nil, check, err
	}
	result := truthy(rightVal)
	check.Result, check.Passed, check.Evaluated = result, result, true
	return result, check, nil
}

func (w *walker) evalUnary(node *ast.UnaryNode) (any, CheckNode, error) {
	check := CheckNode{Expression: exprText(node, w.source), Operator: node.Operator}

	val, childCheck, err := w.eval(node.Node)
	check.Children = []CheckNode{childCheck}
	if err != nil {
		return nil, check, err
	}

	switch node.Operator {
	case "!", "not":
		result := !truthy(val)
		check.Result, check.Passed, check.Evaluated = result, result, true
		return result, check, nil
	default:
		return nil, check, fmt.Errorf("grpolicy: unsupported unary operator %q", node.Operator)
	}
}

func (w *walker) evalIdentifier(node *ast.IdentifierNode) (any, CheckNode, error) {
	check := CheckNode{Expression: exprText(node, w.source)}
	val, ok := w.attrs[node.Value]
	if !ok {
		return nil, check, fmt.Errorf("grpolicy: attribute %q: %w", node.Value, ErrAttributeMissing)
	}
	check.Result, check.Passed, check.Evaluated = val, truthy(val), true
	return val, check, nil
}

func (w *walker) evalArray(node *ast.ArrayNode) (any, CheckNode, error) {
	check := CheckNode{Expression: exprText(node, w.source)}
	values := make([]any, 0, len(node.Nodes))
	for i, el := range node.Nodes {
		val, elCheck, err := w.eval(el)
		check.Children = append(check.Children, elCheck)
		if err != nil {
			// Stub the remaining not-yet-reached elements so Children
			// still mirrors the array literal's full static shape, the
			// same convention evalArgs and every binary-operator eval*
			// follow for their own not-yet-reached operands/arguments.
			for _, remaining := range node.Nodes[i+1:] {
				check.Children = append(check.Children, buildStub(remaining, w.source))
			}
			return nil, check, err
		}
		values = append(values, val)
	}
	check.Result, check.Evaluated = values, true
	check.Passed = len(values) > 0
	return values, check, nil
}

// evalMember implements nested attribute access, both dot (resource.ownerID)
// and bracket (resource["ownerID"], items[0]) syntax — the same AST node
// (*ast.MemberNode) covers both, differing only in Property's runtime value.
// See docs/architecture.md §12 for the full design (sentinel bucketing,
// dot-vs-bracket handling, and the optional-chaining short-circuit
// semantics below).
//
// A nil base fails loud (ErrAttributeMissing) unless node is reached under
// an enclosing "?." (inOptionalScope), in which case it short-circuits to a
// legitimate nil result instead — see inOptionalScope's own doc comment for
// the exact (asymmetric) rule this replicates from expr-lang/expr's own
// parser/VM semantics. "?." only ever guards against nil; a missing map
// key/out-of-range index/unknown-or-unexported struct field is still always
// ErrAttributeMissing regardless of "?." anywhere in the expression — see
// docs/architecture.md §12, Decision A.
func (w *walker) evalMember(node *ast.MemberNode) (any, CheckNode, error) {
	check := CheckNode{Expression: exprText(node, w.source)}

	baseVal, baseCheck, err := w.eval(node.Node)
	if err != nil {
		check.Children = []CheckNode{baseCheck, buildStub(node.Property, w.source)}
		return nil, check, err
	}

	if isNilBase(baseVal) {
		check.Children = []CheckNode{baseCheck, buildStub(node.Property, w.source)}
		if inOptionalScope(node) {
			// Property is never evaluated on this path — no partial
			// evaluation of the rest of the chain past a tunneled nil
			// (proven by testOptionalChainShortCircuit's call-counter
			// discipline, the same style as testShortCircuitAndTrace).
			// Evaluated: true, Result: nil is "this link has a definite,
			// legitimate value: nil" per CheckNode.Result's own doc
			// comment — indistinguishable, by design, from a base that
			// happened to genuinely already be nil (Decision B: there is
			// no way, and no need, to tell "short-circuited" apart from
			// "legitimately nil" from the trace alone).
			check.Evaluated = true
			return nil, check, nil
		}
		return nil, check, fmt.Errorf("grpolicy: member access %s: base value is nil: %w", check.Expression, ErrAttributeMissing)
	}

	propVal, propCheck, err := w.eval(node.Property)
	check.Children = []CheckNode{baseCheck, propCheck}
	if err != nil {
		return nil, check, err
	}

	result, err := resolveMember(baseVal, propVal)
	if err != nil {
		return nil, check, err
	}
	check.Result, check.Passed, check.Evaluated = result, truthy(result), true
	return result, check, nil
}

// evalChain evaluates *ast.ChainNode, the wrapper expr-lang/expr's parser
// uses for a run of one-or-more "?."-containing links (see
// docs/architecture.md §12 for how this was traced through the real parser
// source). It is fully transparent in Decision.Trace — no ChainNode row is
// ever produced, only whatever evalMember returns for its wrapped node.
func (w *walker) evalChain(node *ast.ChainNode) (any, CheckNode, error) {
	if member, ok := node.Node.(*ast.MemberNode); ok {
		return w.evalMember(member)
	}
	// Defensive fallback: expr-lang/expr's grammar only ever wraps a
	// MemberNode (run) in a ChainNode — confirmed empirically while tracing
	// parsePostfixExpression (docs/architecture.md §12) — so this branch is
	// unreachable via any real parser output. Kept so eval never panics on
	// a ChainNode wrapping something else, matching this repo's existing
	// defensive-fallback style for shapes no real parser output produces.
	return w.eval(node.Node)
}

// inOptionalScope reports whether node is reached under an enclosing "?." —
// true if node.Optional itself, or if node.Node is itself a *ast.MemberNode
// for which this is true. This is a purely static, structural predicate
// (no runtime state threaded through eval's recursion needed): tracing
// expr-lang/expr's real parser (parsePostfixExpression, docs/architecture.md
// §12) shows that once any "?." appears in a postfix chain, the parser
// wraps the entire accumulated chain so far — including earlier plain "."
// links — in one ChainNode, re-wrapping at every subsequent step, and the
// compiler (compiler/compiler.go) only ever emits a nil-guard for an
// Optional-flagged MemberNode reached inside such a wrapping. So "has '?.'
// been used at or before this point" is fully determined by the AST shape
// alone: a link written before the first "?." is never protected (Decision
// B), which is exactly what this predicate returns false for.
func inOptionalScope(node *ast.MemberNode) bool {
	if node.Optional {
		return true
	}
	if base, ok := node.Node.(*ast.MemberNode); ok {
		return inOptionalScope(base)
	}
	return false
}

// resolveMember resolves propVal against base — a map key (string or,
// through reflect, any key type a general map[K]V accepts), a slice/array
// index (any integer-kind value, no float truncation), or a struct field
// (string name only, exported fields only — reflection never invokes
// methods and never exposes an unexported field, see docs/architecture.md
// §12). base is dereferenced through any number of pointer levels first
// (each one already confirmed non-nil by evalMember's isNilBase check before
// resolveMember is ever called for the outermost one; a nil pointer found
// while dereferencing a deeper level, e.g. a struct field that is itself a
// nil pointer, is the same ErrAttributeMissing as any other nil base). Any
// other base kind (including a plain Go string, deliberately excluded from
// indexing) is ErrTypeMismatch.
//
// The reflect operations below are guarded by explicit checks wherever
// possible (CanInterface before Interface, AssignableTo/ConvertibleTo before
// Convert, bounds checks before Index) — the recover here is a backstop for
// whatever a gap in those checks misses, not the primary safety mechanism,
// mirroring functionRegistry.call's identical precedent (functions.go) for
// reflect-based custom-function invocation: a recovered panic is a plain
// diagnostic error, not a wrapped sentinel, since reaching it means
// grpolicy's own pre-checks had a gap.
func resolveMember(base, propVal any) (result any, err error) {
	if m, ok := base.(map[string]any); ok {
		key, ok := propVal.(string)
		if !ok {
			return nil, fmt.Errorf("grpolicy: member access: map key has type %T, want string: %w", propVal, ErrTypeMismatch)
		}
		val, found := m[key]
		if !found {
			return nil, fmt.Errorf("grpolicy: member access: key %q not present in map: %w", key, ErrAttributeMissing)
		}
		return val, nil
	}

	defer func() {
		if r := recover(); r != nil {
			result, err = nil, fmt.Errorf("grpolicy: member access: panic: %v", r)
		}
	}()

	rv := reflect.ValueOf(base)
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil, fmt.Errorf("grpolicy: member access: base value is nil: %w", ErrAttributeMissing)
		}
		rv = rv.Elem()
	}

	switch rv.Kind() {
	case reflect.Map:
		return resolveMapMember(rv, propVal)
	case reflect.Slice, reflect.Array:
		return resolveIndexMember(rv, propVal)
	case reflect.Struct:
		return resolveStructMember(rv, propVal)
	default:
		return nil, fmt.Errorf("grpolicy: member access: unsupported base type %T: %w", base, ErrTypeMismatch)
	}
}

func resolveMapMember(rv reflect.Value, propVal any) (any, error) {
	keyType := rv.Type().Key()
	kv := reflect.ValueOf(propVal)
	if !kv.IsValid() {
		return nil, fmt.Errorf("grpolicy: member access: nil map key, want %s: %w", keyType, ErrTypeMismatch)
	}
	if !kv.Type().AssignableTo(keyType) {
		if kv.Type().ConvertibleTo(keyType) && isNumericKind(kv.Kind()) && isNumericKind(keyType.Kind()) {
			kv = kv.Convert(keyType)
		} else {
			return nil, fmt.Errorf("grpolicy: member access: map key has type %T, want %s: %w", propVal, keyType, ErrTypeMismatch)
		}
	}
	val := rv.MapIndex(kv)
	if !val.IsValid() {
		return nil, fmt.Errorf("grpolicy: member access: key %v not present in map: %w", propVal, ErrAttributeMissing)
	}
	return val.Interface(), nil
}

func resolveIndexMember(rv reflect.Value, propVal any) (any, error) {
	idx, ok := toInt(propVal)
	if !ok {
		return nil, fmt.Errorf("grpolicy: member access: index has type %T, want an integer: %w", propVal, ErrTypeMismatch)
	}
	if idx < 0 || idx >= rv.Len() {
		return nil, fmt.Errorf("grpolicy: member access: index %d out of range [0,%d): %w", idx, rv.Len(), ErrAttributeMissing)
	}
	return rv.Index(idx).Interface(), nil
}

// resolveStructMember resolves a string field name against a struct value
// via reflect.Value.FieldByName, which also resolves embedded/promoted
// fields for free (ordinary Go visibility rules). An unknown name and an
// unexported field are both ErrAttributeMissing, deliberately indistinguishable
// from the caller's perspective — an unexported field is exactly as "not
// there" as a nonexistent one, never exposed even by probing for the
// difference between the two error messages.
func resolveStructMember(rv reflect.Value, propVal any) (any, error) {
	name, ok := propVal.(string)
	if !ok {
		return nil, fmt.Errorf("grpolicy: member access: struct field name has type %T, want string: %w", propVal, ErrTypeMismatch)
	}
	field := rv.FieldByName(name)
	if !field.IsValid() {
		return nil, fmt.Errorf("grpolicy: member access: %s has no field %q: %w", rv.Type(), name, ErrAttributeMissing)
	}
	if !field.CanInterface() {
		return nil, fmt.Errorf("grpolicy: member access: %s field %q is unexported: %w", rv.Type(), name, ErrAttributeMissing)
	}
	return field.Interface(), nil
}

// isNilBase reports whether v is nil for member-access purposes — either an
// untyped nil interface, or a typed nil pointer/map/slice/etc. stored in an
// any (which is never == nil as an interface comparison despite being
// logically nil). Checked uniformly before any kind-specific resolution.
func isNilBase(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func, reflect.Interface, reflect.UnsafePointer:
		return rv.IsNil()
	default:
		return false
	}
}

// toInt converts v to an int for slice/array indexing, integer kinds only —
// no float truncation (a float index is a type mismatch, not silently
// truncated). Mirrors toFloat's exhaustive-switch style. int8/int16/int32/
// int64 always fit in int (Go guarantees int is at least 32 bits, and a
// signed type never exceeds a same-or-wider signed int's range); uint8/
// uint16/uint32 likewise always fit an at-least-32-bit int. uint/uint64 are
// the only kinds that can genuinely exceed int's range (on the 64-bit
// platforms grpolicy targets, int is 64-bit but uint/uint64 have a strictly
// larger positive range) — reported as not-an-int (ErrTypeMismatch via the
// caller), never silently wrapped to a negative index.
func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int8:
		return int(n), true
	case int16:
		return int(n), true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case uint:
		if n > math.MaxInt {
			return 0, false
		}
		return int(n), true
	case uint8:
		return int(n), true
	case uint16:
		return int(n), true
	case uint32:
		return int(n), true
	case uint64:
		if n > math.MaxInt {
			return 0, false
		}
		return int(n), true
	default:
		return 0, false
	}
}

func (w *walker) evalArgs(nodes []ast.Node) ([]any, []CheckNode, error) {
	values := make([]any, 0, len(nodes))
	checks := make([]CheckNode, 0, len(nodes))
	for i, n := range nodes {
		val, check, err := w.eval(n)
		checks = append(checks, check)
		if err != nil {
			// See evalArray's comment: stub the remaining arguments so
			// Children still mirrors the call's full static shape.
			for _, remaining := range nodes[i+1:] {
				checks = append(checks, buildStub(remaining, w.source))
			}
			return nil, checks, err
		}
		values = append(values, val)
	}
	return values, checks, nil
}

func (w *walker) evalCall(node *ast.CallNode) (any, CheckNode, error) {
	check := CheckNode{Expression: exprText(node, w.source)}

	ident, ok := node.Callee.(*ast.IdentifierNode)
	if !ok {
		return nil, check, fmt.Errorf("grpolicy: unsupported call target: %T: %w", node.Callee, ErrCompileFailed)
	}

	args, argChecks, err := w.evalArgs(node.Arguments)
	check.Children = argChecks
	if err != nil {
		return nil, check, err
	}

	result, err := w.fns.call(ident.Value, args)
	if err != nil {
		return nil, check, err
	}
	check.Result, check.Passed, check.Evaluated = result, truthy(result), true
	return result, check, nil
}

func (w *walker) evalBuiltin(node *ast.BuiltinNode) (any, CheckNode, error) {
	check := CheckNode{Expression: exprText(node, w.source)}

	args, argChecks, err := w.evalArgs(node.Arguments)
	check.Children = argChecks
	if err != nil {
		return nil, check, err
	}

	var result any
	switch node.Name {
	case "now":
		if len(args) != 0 {
			return nil, check, fmt.Errorf("grpolicy: now(): got %d arguments, want 0: %w", len(args), ErrTypeMismatch)
		}
		result = time.Now()
	case "duration":
		if len(args) != 1 {
			return nil, check, fmt.Errorf("grpolicy: duration(): got %d arguments, want 1: %w", len(args), ErrTypeMismatch)
		}
		s, ok := args[0].(string)
		if !ok {
			return nil, check, fmt.Errorf("grpolicy: duration(): argument has type %T, want string: %w", args[0], ErrTypeMismatch)
		}
		d, perr := time.ParseDuration(s)
		if perr != nil {
			return nil, check, fmt.Errorf("grpolicy: duration(%q): %w", s, perr)
		}
		result = d
	default:
		return nil, check, fmt.Errorf("grpolicy: %q: %w", node.Name, ErrUnknownFunction)
	}

	check.Result, check.Passed, check.Evaluated = result, truthy(result), true
	return result, check, nil
}

func (w *walker) evalComparison(node *ast.BinaryNode) (any, CheckNode, error) {
	check := CheckNode{Expression: exprText(node, w.source), Operator: node.Operator}

	leftVal, leftCheck, err := w.eval(node.Left)
	if err != nil {
		check.Children = []CheckNode{leftCheck, buildStub(node.Right, w.source)}
		return nil, check, err
	}
	rightVal, rightCheck, err := w.eval(node.Right)
	check.Children = []CheckNode{leftCheck, rightCheck}
	if err != nil {
		return nil, check, err
	}

	result, err := compareValues(node.Operator, leftVal, rightVal)
	if err != nil {
		return nil, check, err
	}
	check.Result, check.Passed, check.Evaluated = result, result, true
	return result, check, nil
}

func (w *walker) evalStringOp(node *ast.BinaryNode) (any, CheckNode, error) {
	check := CheckNode{Expression: exprText(node, w.source), Operator: node.Operator}

	leftVal, leftCheck, err := w.eval(node.Left)
	if err != nil {
		check.Children = []CheckNode{leftCheck, buildStub(node.Right, w.source)}
		return nil, check, err
	}
	rightVal, rightCheck, err := w.eval(node.Right)
	check.Children = []CheckNode{leftCheck, rightCheck}
	if err != nil {
		return nil, check, err
	}

	left, lok := leftVal.(string)
	right, rok := rightVal.(string)
	if !lok || !rok {
		return nil, check, fmt.Errorf("grpolicy: %s: operands have type %T, %T, want string, string: %w", node.Operator, leftVal, rightVal, ErrTypeMismatch)
	}

	var result bool
	switch node.Operator {
	case "contains":
		result = strings.Contains(left, right)
	case "startsWith":
		result = strings.HasPrefix(left, right)
	case "endsWith":
		result = strings.HasSuffix(left, right)
	}
	check.Result, check.Passed, check.Evaluated = result, result, true
	return result, check, nil
}

func (w *walker) evalMatches(node *ast.BinaryNode) (any, CheckNode, error) {
	check := CheckNode{Expression: exprText(node, w.source), Operator: "matches"}

	leftVal, leftCheck, err := w.eval(node.Left)
	if err != nil {
		check.Children = []CheckNode{leftCheck, buildStub(node.Right, w.source)}
		return nil, check, err
	}
	rightVal, rightCheck, err := w.eval(node.Right)
	check.Children = []CheckNode{leftCheck, rightCheck}
	if err != nil {
		return nil, check, err
	}

	subject, sok := leftVal.(string)
	pattern, pok := rightVal.(string)
	if !sok || !pok {
		return nil, check, fmt.Errorf("grpolicy: matches: operands have type %T, %T, want string, string: %w", leftVal, rightVal, ErrTypeMismatch)
	}

	re, rerr := w.policy.compiledRegex(pattern)
	if rerr != nil {
		return nil, check, fmt.Errorf("grpolicy: matches: invalid regex %q: %w", pattern, ErrCompileFailed)
	}

	result := re.MatchString(subject)
	check.Result, check.Passed, check.Evaluated = result, result, true
	return result, check, nil
}

func (w *walker) evalIn(node *ast.BinaryNode) (any, CheckNode, error) {
	check := CheckNode{Expression: exprText(node, w.source), Operator: "in"}

	needle, leftCheck, err := w.eval(node.Left)
	if err != nil {
		check.Children = []CheckNode{leftCheck, buildStub(node.Right, w.source)}
		return nil, check, err
	}
	haystack, rightCheck, err := w.eval(node.Right)
	check.Children = []CheckNode{leftCheck, rightCheck}
	if err != nil {
		return nil, check, err
	}

	result, err := membershipCheck(needle, haystack)
	if err != nil {
		return nil, check, err
	}
	check.Result, check.Passed, check.Evaluated = result, result, true
	return result, check, nil
}

// membershipCheck implements "in" against a slice/array (of any element
// type) or a map[string]any (checked by key). Any other haystack type is
// ErrTypeMismatch.
func membershipCheck(needle, haystack any) (bool, error) {
	if haystack == nil {
		return false, nil
	}

	if m, ok := haystack.(map[string]any); ok {
		key, ok := needle.(string)
		if !ok {
			return false, fmt.Errorf("grpolicy: in: map haystack requires a string needle, got %T: %w", needle, ErrTypeMismatch)
		}
		_, found := m[key]
		return found, nil
	}

	hv := reflect.ValueOf(haystack)
	switch hv.Kind() {
	case reflect.Slice, reflect.Array:
		for i := 0; i < hv.Len(); i++ {
			if valuesEqual(needle, hv.Index(i).Interface()) {
				return true, nil
			}
		}
		return false, nil
	default:
		return false, fmt.Errorf("grpolicy: in: unsupported haystack type %T: %w", haystack, ErrTypeMismatch)
	}
}

func valuesEqual(a, b any) bool {
	if eq, err := compareValues("==", a, b); err == nil {
		return eq
	}
	return reflect.DeepEqual(a, b)
}

// compareValues implements ==, !=, <, <=, >, >= across the type categories
// grpolicy explicitly supports (numeric, string, time.Time, bool), with an
// errors.Is(ErrTypeMismatch)-wrapped error for anything else — there is no
// silent coercion between categories (e.g. a numeric attribute compared
// against a string never succeeds by accident).
func compareValues(op string, a, b any) (bool, error) {
	if an, aok := toFloat(a); aok {
		if bn, bok := toFloat(b); bok {
			return numericCompare(op, an, bn)
		}
		return false, fmt.Errorf("grpolicy: comparing %T %s %T: %w", a, op, b, ErrTypeMismatch)
	}
	if as, aok := a.(string); aok {
		if bs, bok := b.(string); bok {
			return stringCompare(op, as, bs)
		}
		return false, fmt.Errorf("grpolicy: comparing %T %s %T: %w", a, op, b, ErrTypeMismatch)
	}
	if at, aok := a.(time.Time); aok {
		if bt, bok := b.(time.Time); bok {
			return timeCompare(op, at, bt)
		}
		return false, fmt.Errorf("grpolicy: comparing %T %s %T: %w", a, op, b, ErrTypeMismatch)
	}
	if ab, aok := a.(bool); aok {
		bb, bok := b.(bool)
		if !bok {
			return false, fmt.Errorf("grpolicy: comparing %T %s %T: %w", a, op, b, ErrTypeMismatch)
		}
		switch op {
		case "==":
			return ab == bb, nil
		case "!=":
			return ab != bb, nil
		default:
			return false, fmt.Errorf("grpolicy: operator %q not supported for bool operands: %w", op, ErrTypeMismatch)
		}
	}

	switch op {
	case "==":
		return reflect.DeepEqual(a, b), nil
	case "!=":
		return !reflect.DeepEqual(a, b), nil
	default:
		return false, fmt.Errorf("grpolicy: comparing %T %s %T: %w", a, op, b, ErrTypeMismatch)
	}
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int8:
		return float64(n), true
	case int16:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint8:
		return float64(n), true
	case uint16:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	case float32:
		return float64(n), true
	case float64:
		return n, true
	default:
		return 0, false
	}
}

func numericCompare(op string, a, b float64) (bool, error) {
	switch op {
	case "==":
		return a == b, nil
	case "!=":
		return a != b, nil
	case "<":
		return a < b, nil
	case "<=":
		return a <= b, nil
	case ">":
		return a > b, nil
	case ">=":
		return a >= b, nil
	default:
		return false, fmt.Errorf("grpolicy: unsupported comparison operator %q", op)
	}
}

func stringCompare(op string, a, b string) (bool, error) {
	switch op {
	case "==":
		return a == b, nil
	case "!=":
		return a != b, nil
	case "<":
		return a < b, nil
	case "<=":
		return a <= b, nil
	case ">":
		return a > b, nil
	case ">=":
		return a >= b, nil
	default:
		return false, fmt.Errorf("grpolicy: unsupported comparison operator %q", op)
	}
}

func timeCompare(op string, a, b time.Time) (bool, error) {
	switch op {
	case "==":
		return a.Equal(b), nil
	case "!=":
		return !a.Equal(b), nil
	case "<":
		return a.Before(b), nil
	case "<=":
		return a.Before(b) || a.Equal(b), nil
	case ">":
		return a.After(b), nil
	case ">=":
		return a.After(b) || a.Equal(b), nil
	default:
		return false, fmt.Errorf("grpolicy: unsupported comparison operator %q", op)
	}
}

// truthy coerces a value to boolean for &&/||/! and for CheckNode.Passed.
// nil is false; bool passes through; numeric zero and empty string are
// false; anything else (slices, maps, time.Time, non-zero numbers,
// non-empty strings) is true.
func truthy(v any) bool {
	switch val := v.(type) {
	case nil:
		return false
	case bool:
		return val
	case string:
		return val != ""
	default:
		if f, ok := toFloat(v); ok {
			return f != 0
		}
		return true
	}
}
