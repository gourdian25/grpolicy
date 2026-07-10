// File: ast_helpers.go

package grpolicy

import "github.com/expr-lang/expr/ast"

// children returns n's direct child nodes in source order. It is the single
// place that knows how to descend into each ast.Node kind grpolicy supports,
// used by both measureComplexity (at Compile time) and eval.go's buildStub
// (at Evaluate time) so the two can never silently drift out of sync about
// what counts as "part of the tree."
func children(n ast.Node) []ast.Node {
	switch node := n.(type) {
	case *ast.BinaryNode:
		return []ast.Node{node.Left, node.Right}
	case *ast.UnaryNode:
		return []ast.Node{node.Node}
	case *ast.CallNode:
		return node.Arguments
	case *ast.BuiltinNode:
		return node.Arguments
	case *ast.ArrayNode:
		return node.Nodes
	default:
		// Leaf nodes: IdentifierNode, StringNode, IntegerNode, FloatNode,
		// BoolNode, NilNode, and anything else grpolicy doesn't specially
		// evaluate.
		return nil
	}
}

// measureComplexity walks n once, counting total node visits and maximum
// nesting depth. It is run once per Compile call, before a compiledPolicy is
// ever constructed, so a pathologically large or deep expression is rejected
// before it can reach Evaluate.
func measureComplexity(n ast.Node) (nodeCount, maxDepth int) {
	var walk func(node ast.Node, depth int)
	walk = func(node ast.Node, depth int) {
		if node == nil {
			return
		}
		nodeCount++
		if depth > maxDepth {
			maxDepth = depth
		}
		for _, c := range children(node) {
			walk(c, depth+1)
		}
	}
	walk(n, 1)
	return nodeCount, maxDepth
}

// nodeSpan returns the [from, to) byte range in the original source that n
// and all of its descendants (per children, plus a CallNode's Callee, which
// children deliberately omits since it isn't an evaluated sub-expression)
// span. ast.Node.Location() alone is not sufficient for this: expr-lang/expr
// gives a BinaryNode's own Location as just its operator token's span (e.g.
// "&&"), not the full left-to-right range — confirmed empirically against
// the actual parser output, not assumed.
func nodeSpan(n ast.Node) (from, to int) {
	loc := n.Location()
	from, to = loc.From, loc.To

	extra := children(n)
	if call, ok := n.(*ast.CallNode); ok {
		extra = append([]ast.Node{call.Callee}, extra...)
	}
	for _, c := range extra {
		if c == nil {
			continue
		}
		cf, ct := nodeSpan(c)
		if cf < from {
			from = cf
		}
		if ct > to {
			to = ct
		}
	}
	return from, to
}

// exprText returns the source text n spans, using nodeSpan. Returns "" if
// the computed span is out of bounds (should not happen for a tree parsed
// from source, but exprText never panics regardless).
func exprText(n ast.Node, source string) string {
	from, to := nodeSpan(n)
	if from < 0 || to > len(source) || from > to {
		return ""
	}
	return source[from:to]
}

// operatorOf returns the operator string a node represents for
// CheckNode.Operator — "&&", "==", "in", etc. for Binary/Unary nodes, or ""
// for anything else (identifiers, literals, calls), per CheckNode's doc
// comment.
func operatorOf(n ast.Node) string {
	switch node := n.(type) {
	case *ast.BinaryNode:
		return node.Operator
	case *ast.UnaryNode:
		return node.Operator
	default:
		return ""
	}
}
