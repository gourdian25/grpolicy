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
	case *ast.MemberNode:
		return []ast.Node{node.Node, node.Property}
	case *ast.ChainNode:
		return []ast.Node{node.Node}
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
// spans. ast.Node.Location() alone is not sufficient for this: expr-lang/expr
// gives a BinaryNode's own Location as just its operator token's span (e.g.
// "&&"), not the full left-to-right range, and gives a CallNode/BuiltinNode's
// own Location as just its bare name (e.g. "isBusinessHours", not
// "isBusinessHours(hour)") — confirmed empirically against real parser
// output, not assumed, including the fact that neither ever includes a
// trailing closing delimiter contributed by punctuation alone (no argument's
// own span reaches the ")"/"]" itself).
//
// CallNode/BuiltinNode/ArrayNode are therefore handled by locating the
// opening "("/"[" and scanning forward for its balanced match, rather than
// by unioning children's spans (which would still miss the closing
// delimiter, since it belongs to no child node). Every other kind unions
// children's spans (via children()), which is sufficient for them since
// expr-lang's grammar never adds trailing punctuation after a BinaryNode's
// or UnaryNode's last operand.
func nodeSpan(n ast.Node, source string) (from, to int) {
	switch node := n.(type) {
	case *ast.CallNode:
		// The Callee's own span always starts at the function name;
		// scanning from there finds the call's opening paren reliably even
		// though CallNode.Location() itself only covers the bare name.
		nameFrom, nameTo := nodeSpan(node.Callee, source)
		return nameFrom, matchClosingFrom(source, nameTo, '(', ')')
	case *ast.BuiltinNode:
		loc := node.Location()
		return loc.From, matchClosingFrom(source, loc.To, '(', ')')
	case *ast.ArrayNode:
		loc := node.Location()
		// ArrayNode.Location() starts at "[" itself (confirmed empirically).
		return loc.From, matchClosingFrom(source, loc.From, '[', ']')
	case *ast.ChainNode:
		// ChainNode is a transparent wrapper around an optional-chaining
		// MemberNode run (see docs/architecture.md §12) — its own Location()
		// covers only a fragment of the chain, so its span is exactly its
		// wrapped node's own (already-correct) span.
		return nodeSpan(node.Node, source)
	case *ast.MemberNode:
		// MemberNode.Location() covers only the property name (dot form) or
		// the bare "[" (bracket form) — neither is useful directly (see
		// docs/architecture.md §12). Dot form's correct span falls out of
		// the generic children-span-union path below once Property is in
		// children(), so only bracket form needs special handling here: look
		// for a "[" immediately after the base's span and, if found, match
		// its balanced close (the same trailing-delimiter problem already
		// fixed for CallNode/BuiltinNode/ArrayNode). matchClosingFrom returns
		// its input unchanged when no "[" is found there, which is exactly
		// how dot form is told apart from bracket form: no new AST field
		// distinguishes them, so this has to be detected structurally.
		nodeFrom, nodeTo := nodeSpan(node.Node, source)
		if to := matchClosingFrom(source, nodeTo, '[', ']'); to != nodeTo {
			return nodeFrom, to
		}
		// Dot form: fall through to the generic path below.
	}

	loc := n.Location()
	from, to = loc.From, loc.To
	for _, c := range children(n) {
		if c == nil {
			continue
		}
		cf, ct := nodeSpan(c, source)
		if cf < from {
			from = cf
		}
		if ct > to {
			to = ct
		}
	}
	return from, to
}

// matchClosingFrom scans source starting at searchFrom (skipping leading
// whitespace) for openCh, then returns the index just past openCh's
// balanced matching closeCh — tracking nested "(" / "[" of either kind
// uniformly (valid parsed syntax is always properly nested, so the first
// return-to-zero-depth close after the opener is guaranteed to be its true
// match) and skipping over double-quoted string contents so a bracket
// character inside a string literal argument is never miscounted. Returns
// searchFrom unchanged (a defensive no-op) if openCh isn't found or the
// bracket is unbalanced — exprText below never panics regardless.
func matchClosingFrom(source string, searchFrom int, openCh, closeCh byte) int {
	i := searchFrom
	for i < len(source) && (source[i] == ' ' || source[i] == '\t' || source[i] == '\n' || source[i] == '\r') {
		i++
	}
	if i >= len(source) || source[i] != openCh {
		return searchFrom
	}

	depth := 0
	inString := false
	for ; i < len(source); i++ {
		c := source[i]
		if inString {
			if c == '\\' {
				i++
				continue
			}
			if c == '"' {
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '(', '[':
			depth++
		case ')', ']':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return searchFrom
}

// exprText returns the source text n spans, using nodeSpan. Returns "" if
// the computed span is out of bounds (should not happen for a tree parsed
// from source, but exprText never panics regardless).
func exprText(n ast.Node, source string) string {
	from, to := nodeSpan(n, source)
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
