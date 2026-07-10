// File: complexity_internal_test.go

package grpolicy

import (
	"errors"
	"testing"

	"github.com/expr-lang/expr/parser"
)

// TestComplexityLimitBoundaries is a white-box test (internal package, not
// grpolicy_test) so it can call measureComplexity directly to learn the
// exact node count and depth of a candidate expression, then configure an
// engine's limits to precisely that value — proving the boundary is
// inclusive (exactly at the limit passes) and exclusive one past it
// (Compile fails), and that node count and nesting depth are each enforced
// independently rather than only one dimension actually being checked.
func TestComplexityLimitBoundaries(t *testing.T) {
	const expr = `a == 1 && (b == 2 || c == 3)`
	tree, err := parser.Parse(expr)
	if err != nil {
		t.Fatalf("parser.Parse: %v", err)
	}
	nodeCount, depth := measureComplexity(tree.Node)
	if nodeCount == 0 || depth == 0 {
		t.Fatalf("measureComplexity returned nodeCount=%d depth=%d, want both > 0", nodeCount, depth)
	}

	t.Run("ExactlyAtNodeAndDepthLimitCompiles", func(t *testing.T) {
		eng, err := NewEngine(WithMaxComplexity(nodeCount, depth))
		if err != nil {
			t.Fatalf("NewEngine: %v", err)
		}
		defer eng.Close()
		if _, err := eng.Compile(expr); err != nil {
			t.Fatalf("Compile at exact limit (nodes=%d depth=%d) failed: %v", nodeCount, depth, err)
		}
	})

	t.Run("OneNodeUnderLimitRejectsOnNodeCountAlone", func(t *testing.T) {
		eng, err := NewEngine(WithMaxComplexity(nodeCount-1, depth))
		if err != nil {
			t.Fatalf("NewEngine: %v", err)
		}
		defer eng.Close()
		_, err = eng.Compile(expr)
		if !errors.Is(err, ErrTooComplex) {
			t.Fatalf("Compile with maxNodes=%d (one under actual %d, depth limit unchanged) error = %v, want ErrTooComplex", nodeCount-1, nodeCount, err)
		}
	})

	t.Run("OneDepthUnderLimitRejectsOnDepthAlone", func(t *testing.T) {
		eng, err := NewEngine(WithMaxComplexity(nodeCount, depth-1))
		if err != nil {
			t.Fatalf("NewEngine: %v", err)
		}
		defer eng.Close()
		_, err = eng.Compile(expr)
		if !errors.Is(err, ErrTooComplex) {
			t.Fatalf("Compile with maxDepth=%d (one under actual %d, node limit unchanged) error = %v, want ErrTooComplex", depth-1, depth, err)
		}
	})

	t.Run("WellUnderBothLimitsCompiles", func(t *testing.T) {
		eng, err := NewEngine(WithMaxComplexity(nodeCount+10, depth+10))
		if err != nil {
			t.Fatalf("NewEngine: %v", err)
		}
		defer eng.Close()
		if _, err := eng.Compile(expr); err != nil {
			t.Fatalf("Compile well under limits failed: %v", err)
		}
	})
}
