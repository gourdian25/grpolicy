// File: functions.go

package grpolicy

import (
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/expr-lang/expr/builtin"
)

// supportedBuiltinFunctions are the identifiers grpolicy implements directly
// as *ast.BuiltinNode handlers in eval.go. expr-lang/expr's parser reserves
// certain bare identifiers (see github.com/expr-lang/expr/builtin.Index —
// "now" and "duration" among them) and always parses a call to one of them
// as a BuiltinNode, never a CallNode, regardless of whether a same-named
// function is registered via RegisterFunction. grpolicy therefore cannot
// route "now"/"duration" through the reflect-based functionRegistry at all —
// they are implemented as their own case in eval.go instead. This is a
// hard constraint of the underlying parser, not a design choice.
var supportedBuiltinFunctions = map[string]bool{
	"now":      true,
	"duration": true,
}

// functionRegistry holds the named functions a compiled policy is allowed to
// call via a CallNode. Since grpolicy's evaluator walks the AST directly
// instead of using expr-lang/expr's own Run/Program execution path, none of
// that library's compile-time function-signature validation applies here —
// arity and argument-type checking in call() below is the only enforcement
// grpolicy gets, and it is grpolicy's own responsibility to get it right.
type functionRegistry struct {
	mu  sync.RWMutex
	fns map[string]reflect.Value
}

func newFunctionRegistry() *functionRegistry {
	r := &functionRegistry{fns: make(map[string]reflect.Value)}
	mustRegisterBuiltin(r, "before", func(a, b time.Time) bool { return a.Before(b) })
	mustRegisterBuiltin(r, "after", func(a, b time.Time) bool { return a.After(b) })
	mustRegisterBuiltin(r, "addDuration", func(t time.Time, d time.Duration) time.Time { return t.Add(d) })
	return r
}

// mustRegisterBuiltin registers one of grpolicy's own hardcoded, known-good
// built-ins. register can only fail for a non-function value or a reserved
// name — neither is possible for these literal, fixed calls, so a failure
// here would be a grpolicy programming bug, not a runtime condition; panic
// rather than silently discard the error (the same "this cannot fail"
// idiom as regexp.MustCompile).
func mustRegisterBuiltin(r *functionRegistry, name string, fn any) {
	if err := r.register(name, fn); err != nil {
		panic(fmt.Sprintf("grpolicy: internal built-in registration failed for %q: %v", name, err))
	}
}

// register stores fn under name, overwriting any existing registration.
// Used both for grpolicy's built-ins at construction and for
// engine.RegisterFunction. Registering any name that collides with an
// expr-lang/expr reserved builtin identifier is rejected outright — this
// includes "now" and "duration", not just the other reserved names: a call
// to any reserved identifier always parses as a BuiltinNode, never a
// CallNode, so a same-named registered function could never be reached
// either way. For "now"/"duration" specifically, grpolicy already
// implements them directly as BuiltinNode handlers in eval.go; for every
// other reserved name, grpolicy simply doesn't support it. Either way,
// silently accepting the registration would be a footgun (registered but
// permanently unreachable) — fail loud at registration time instead.
func (r *functionRegistry) register(name string, fn any) error {
	v := reflect.ValueOf(fn)
	if v.Kind() != reflect.Func {
		return fmt.Errorf("grpolicy: registering %q: value is not a function", name)
	}
	if _, reserved := builtin.Index[name]; reserved {
		if supportedBuiltinFunctions[name] {
			return fmt.Errorf("grpolicy: registering %q: name is an expr-lang/expr reserved builtin identifier that grpolicy already implements directly as a built-in; it cannot be overridden via RegisterFunction", name)
		}
		return fmt.Errorf("grpolicy: registering %q: name collides with an expr-lang/expr reserved builtin identifier; calls to %q always parse as a builtin and can never reach a registered function", name, name)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fns[name] = v
	return nil
}

// has reports whether name is registered, used by Compile to validate every
// CallNode's callee up front.
func (r *functionRegistry) has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.fns[name]
	return ok
}

// call invokes the registered function named name with args, converting
// panics from the reflected call (a badly-typed or misbehaving custom
// function) into a returned error instead of crashing the process.
func (r *functionRegistry) call(name string, args []any) (result any, err error) {
	r.mu.RLock()
	fn, ok := r.fns[name]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("grpolicy: calling %q: %w", name, ErrUnknownFunction)
	}

	fnType := fn.Type()
	if fnType.IsVariadic() {
		if len(args) < fnType.NumIn()-1 {
			return nil, fmt.Errorf("grpolicy: calling %q: got %d arguments, want at least %d: %w", name, len(args), fnType.NumIn()-1, ErrTypeMismatch)
		}
	} else if len(args) != fnType.NumIn() {
		return nil, fmt.Errorf("grpolicy: calling %q: got %d arguments, want %d: %w", name, len(args), fnType.NumIn(), ErrTypeMismatch)
	}

	in := make([]reflect.Value, len(args))
	for i, a := range args {
		paramIdx := i
		if fnType.IsVariadic() && paramIdx >= fnType.NumIn()-1 {
			paramIdx = fnType.NumIn() - 1
		}
		want := fnType.In(paramIdx)
		if fnType.IsVariadic() && paramIdx == fnType.NumIn()-1 {
			want = want.Elem()
		}
		av := reflect.ValueOf(a)
		if !av.IsValid() {
			in[i] = reflect.Zero(want)
			continue
		}
		if !av.Type().AssignableTo(want) {
			if av.Type().ConvertibleTo(want) && isNumericKind(av.Kind()) && isNumericKind(want.Kind()) {
				av = av.Convert(want)
			} else {
				return nil, fmt.Errorf("grpolicy: calling %q: argument %d has type %s, want %s: %w", name, i, av.Type(), want, ErrTypeMismatch)
			}
		}
		in[i] = av
	}

	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("grpolicy: calling %q: panic: %v", name, r)
			result = nil
		}
	}()

	out := fn.Call(in)
	switch len(out) {
	case 0:
		return nil, nil
	case 1:
		if e, ok := out[0].Interface().(error); ok {
			return nil, e
		}
		return out[0].Interface(), nil
	case 2:
		var callErr error
		if e, ok := out[1].Interface().(error); ok {
			callErr = e
		}
		return out[0].Interface(), callErr
	default:
		return nil, fmt.Errorf("grpolicy: calling %q: function returns %d values, want at most 2 (value, error)", name, len(out))
	}
}

func isNumericKind(k reflect.Kind) bool {
	switch k {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	default:
		return false
	}
}
