// File: example/example.go

// Command example is a runnable demonstration of grpolicy — no external
// services required (grpolicy is a pure computation engine with no storage
// backend, unlike grcache/graudit's examples). It shows Compile, Evaluate,
// RegisterFunction, and — importantly — what Decision.Trace actually looks
// like, including a short-circuited branch.
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/gourdian25/grlog"

	"github.com/gourdian25/grpolicy"
)

func main() {
	// *grlog.Logger satisfies grpolicy.Logger with no adapter — the same
	// zero-import pattern every sibling repo uses (grpolicy's own code
	// never imports grlog; only this example does, to demonstrate it).
	logger := grlog.NewDefaultLogger()

	engine, err := grpolicy.NewEngine(grpolicy.WithLogger(logger))
	if err != nil {
		log.Fatal(err)
	}
	defer engine.Close()

	// A custom function a real grauth-style consumer might register —
	// grpolicy has no concept of "business hours" itself, only attributes.
	if err := engine.RegisterFunction("isBusinessHours", func(hour int) bool {
		return hour >= 9 && hour < 17
	}); err != nil {
		log.Fatal(err)
	}

	policy, err := engine.Compile(`age >= 18 && country in allowedCountries && isBusinessHours(hour)`)
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	fmt.Println("--- An allowed decision ---")
	allowed, err := policy.Evaluate(ctx, map[string]any{
		"age":              21,
		"country":          "US",
		"allowedCountries": []string{"US", "CA"},
		"hour":             14,
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Allowed:", allowed.Allowed)
	fmt.Println("Trace:")
	for _, node := range allowed.Trace {
		printTrace(node, 0)
	}

	// age fails first, so && short-circuits: neither the "country in
	// allowedCountries" nor the "isBusinessHours(hour)" branch is ever
	// evaluated. Both still appear in Trace, marked Evaluated: false with a
	// nil Result — see the "precise claim" section in README.md for exactly
	// what this does and doesn't mean.
	fmt.Println("\n--- A denied decision (age check fails, short-circuiting the rest) ---")
	denied, err := policy.Evaluate(ctx, map[string]any{
		"age":              15,
		"country":          "US",
		"allowedCountries": []string{"US", "CA"},
		"hour":             14,
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Allowed:", denied.Allowed)
	fmt.Println("Trace:")
	for _, node := range denied.Trace {
		printTrace(node, 0)
	}

	// A missing attribute fails loud rather than silently defaulting —
	// Evaluate returns a non-nil error, and the partial Trace built up to
	// the point of failure comes back alongside it.
	fmt.Println("\n--- A missing attribute (fails loud, partial trace still returned) ---")
	_, err = policy.Evaluate(ctx, map[string]any{"country": "US"})
	fmt.Println("Error:", err)
}

func printTrace(node grpolicy.CheckNode, depth int) {
	indent := ""
	for i := 0; i < depth; i++ {
		indent += "  "
	}
	fmt.Printf("%s%-45s op=%-10q evaluated=%-5v passed=%-5v result=%#v\n",
		indent, node.Expression, node.Operator, node.Evaluated, node.Passed, node.Result)
	for _, child := range node.Children {
		printTrace(child, depth+1)
	}
}
