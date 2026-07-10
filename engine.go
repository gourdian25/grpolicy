// File: engine.go

package grpolicy

import (
	"fmt"
	"regexp"
	"sync"
	"sync/atomic"

	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/parser"
)

// defaultMaxNodes and defaultMaxDepth are generous enough for realistic
// ABAC-style expressions while still rejecting adversarially large or deep
// input at Compile time.
const (
	defaultMaxNodes = 1000
	defaultMaxDepth = 100
)

// Engine compiles and caches policy expressions. A single Engine is safe
// for concurrent use by multiple goroutines.
type Engine interface {
	// Compile parses and validates expr, returning a reusable compiled
	// form. Results are cached internally by source string — calling
	// Compile with identical expr text repeatedly is cheap. Every function
	// referenced by expr is validated against the registry at this point,
	// not deferred to Evaluate.
	//
	// Parameters:
	//   - expr: string — the policy expression source text
	//
	// Returns:
	//   - CompiledPolicy: a reusable, concurrency-safe compiled form
	//   - error: ErrCompileFailed if expr does not parse, ErrTooComplex if
	//     it exceeds the configured node-count/depth limit,
	//     ErrUnknownFunction if it calls a name not registered via
	//     RegisterFunction, ErrClosed if called after Close
	//
	// Example:
	//
	//	policy, err := engine.Compile(`age >= 18 && country in allowedCountries`)
	Compile(expr string) (CompiledPolicy, error)

	// RegisterFunction adds a custom function usable by policies compiled
	// after this call. Must be called before Compile for any policy that
	// references the function name — a Compile call for an expression using
	// an as-yet-unregistered name fails with ErrUnknownFunction even if the
	// function is registered later; grpolicy does not retroactively
	// re-validate already-compiled or already-cached policies.
	//
	// Parameters:
	//   - name: string — the function name as it appears in policy source
	//   - fn: any — a Go function value; arity/argument types are validated
	//     via reflection on every call, not at registration time
	//
	// Returns:
	//   - error: non-nil if fn is not a function, or ErrClosed if called
	//     after Close
	RegisterFunction(name string, fn any) error

	// Close marks the engine closed; every subsequent Compile/RegisterFunction
	// call returns ErrClosed. Close is idempotent — calling it more than
	// once is safe and returns nil every time. Close is currently a no-op
	// beyond marking the engine closed, since v0.1.0 has no goroutines or
	// connections to release; the method exists now so a future
	// cache-sweeper or metrics-flush can be added later without a breaking
	// API change.
	Close() error
}

// EngineOption configures NewEngine.
type EngineOption func(*engineConfig)

type engineConfig struct {
	logger      Logger
	cacheSize   int
	unbounded   bool
	maxNodes    int
	maxDepth    int
}

// WithLogger sets the Logger used for optional diagnostic messages. The
// default is NopLogger(), which discards everything.
func WithLogger(l Logger) EngineOption {
	return func(c *engineConfig) { c.logger = l }
}

// WithCacheSize sets the maximum number of distinct compiled policies the
// engine caches by source string. size <= 0 falls back to the default
// (512) — it is never silently treated as unbounded. Use WithUnboundedCache
// to explicitly opt into no limit.
func WithCacheSize(size int) EngineOption {
	return func(c *engineConfig) { c.cacheSize = size }
}

// WithUnboundedCache disables the compiled-policy cache's size limit
// entirely. This is an explicit opt-in, since an unbounded cache is a
// memory-growth risk if the caller ever compiles many distinct
// dynamically-generated policy strings.
func WithUnboundedCache() EngineOption {
	return func(c *engineConfig) { c.unbounded = true }
}

// WithMaxComplexity sets the maximum AST node count and nesting depth a
// policy may have. Exceeding either limit fails Compile with ErrTooComplex.
// Defaults are 1000 nodes and depth 100.
func WithMaxComplexity(maxNodes, maxDepth int) EngineOption {
	return func(c *engineConfig) { c.maxNodes = maxNodes; c.maxDepth = maxDepth }
}

type engine struct {
	logger    Logger
	cache     *policyCache
	functions *functionRegistry
	maxNodes  int
	maxDepth  int

	closed    atomic.Bool
	closeOnce sync.Once
}

// NewEngine constructs an Engine ready to compile and evaluate policies.
//
// Parameters:
//   - opts: ...EngineOption — see WithLogger, WithCacheSize,
//     WithUnboundedCache, WithMaxComplexity
//
// Returns:
//   - Engine
//   - error: currently always nil; reserved for future validation of opts
//
// Example:
//
//	engine, err := grpolicy.NewEngine()
func NewEngine(opts ...EngineOption) (Engine, error) {
	cfg := &engineConfig{
		cacheSize: defaultCacheSize,
		maxNodes:  defaultMaxNodes,
		maxDepth:  defaultMaxDepth,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	cacheSize := cfg.cacheSize
	if cfg.unbounded {
		cacheSize = 0
	} else if cacheSize <= 0 {
		cacheSize = defaultCacheSize
	}

	return &engine{
		logger:    OrNop(cfg.logger),
		cache:     newPolicyCache(cacheSize),
		functions: newFunctionRegistry(),
		maxNodes:  cfg.maxNodes,
		maxDepth:  cfg.maxDepth,
	}, nil
}

func (e *engine) Compile(expr string) (CompiledPolicy, error) {
	if e.closed.Load() {
		return nil, fmt.Errorf("grpolicy: compiling %q: %w", expr, ErrClosed)
	}

	if cached, ok := e.cache.get(expr); ok {
		return cached, nil
	}

	tree, err := parser.Parse(expr)
	if err != nil {
		return nil, fmt.Errorf("grpolicy: compiling %q: %w: %v", expr, ErrCompileFailed, err)
	}

	nodeCount, depth := measureComplexity(tree.Node)
	if nodeCount > e.maxNodes || depth > e.maxDepth {
		return nil, fmt.Errorf("grpolicy: compiling %q: expression has %d nodes (max %d) at depth %d (max %d): %w",
			expr, nodeCount, e.maxNodes, depth, e.maxDepth, ErrTooComplex)
	}

	if err := validateFunctionCalls(tree.Node, e.functions); err != nil {
		return nil, fmt.Errorf("grpolicy: compiling %q: %w", expr, err)
	}

	p := &compiledPolicy{
		source:     expr,
		root:       tree.Node,
		engine:     e,
		nodeCount:  nodeCount,
		depth:      depth,
		regexCache: make(map[string]*regexp.Regexp),
	}
	e.cache.put(expr, p)
	e.logger.Infof("grpolicy: compiled policy %q (%d nodes, depth %d)", expr, nodeCount, depth)
	return p, nil
}

func (e *engine) RegisterFunction(name string, fn any) error {
	if e.closed.Load() {
		return fmt.Errorf("grpolicy: registering %q: %w", name, ErrClosed)
	}
	return e.functions.register(name, fn)
}

func (e *engine) Close() error {
	e.closeOnce.Do(func() {
		e.closed.Store(true)
		e.logger.Infof("grpolicy: engine closed")
	})
	return nil
}

// validateFunctionCalls walks n and fails with ErrUnknownFunction if any
// CallNode references a name not present in fns. This runs once at Compile
// time so a bad function reference never survives to Evaluate.
func validateFunctionCalls(n ast.Node, fns *functionRegistry) error {
	if n == nil {
		return nil
	}
	switch node := n.(type) {
	case *ast.CallNode:
		ident, ok := node.Callee.(*ast.IdentifierNode)
		if !ok {
			return fmt.Errorf("grpolicy: unsupported call target: %T", node.Callee)
		}
		if !fns.has(ident.Value) {
			return fmt.Errorf("grpolicy: %q: %w", ident.Value, ErrUnknownFunction)
		}
	case *ast.BuiltinNode:
		if !supportedBuiltinFunctions[node.Name] {
			return fmt.Errorf("grpolicy: %q: %w", node.Name, ErrUnknownFunction)
		}
	}
	for _, c := range children(n) {
		if err := validateFunctionCalls(c, fns); err != nil {
			return err
		}
	}
	return nil
}
