// File: eval.go

package grpolicy

import (
	"context"
	"fmt"
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
	for _, el := range node.Nodes {
		val, elCheck, err := w.eval(el)
		check.Children = append(check.Children, elCheck)
		if err != nil {
			return nil, check, err
		}
		values = append(values, val)
	}
	check.Result, check.Evaluated = values, true
	check.Passed = len(values) > 0
	return values, check, nil
}

func (w *walker) evalArgs(nodes []ast.Node) ([]any, []CheckNode, error) {
	values := make([]any, 0, len(nodes))
	checks := make([]CheckNode, 0, len(nodes))
	for _, n := range nodes {
		val, check, err := w.eval(n)
		checks = append(checks, check)
		if err != nil {
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
		return nil, check, fmt.Errorf("grpolicy: unsupported call target: %T", node.Callee)
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
