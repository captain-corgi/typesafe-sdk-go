// Command duplicate-scenarios suggests semantic duplicate test rows after
// comparing exact inputs and assertions in Go. It never removes a test.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/captain-corgi/typesafe-sdk-go"
)

type scenario struct{ name, input, assertion string }

func main() {
	if os.Getenv("TYPESAFE_API_KEY") == "" {
		log.Fatal("set TYPESAFE_API_KEY for this opt-in review command")
	}
	client, err := typesafe.NewClient()
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	a := scenario{"reject blank token", "token=\"\"", "ErrUnauthorized; no handler call"}
	b := scenario{"missing auth header", "Authorization absent", "ErrUnauthorized; no handler call"}
	if a.input == b.input && a.assertion == b.assertion {
		fmt.Printf("exact duplicate candidate: %s / %s; reviewer decides whether to combine\n", a.name, b.name)
		return
	}
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: map[string]any{"case_a": map[string]any{"name": a.name, "input": a.input, "assertion": a.assertion}, "case_b": map[string]any{"name": b.name, "input": b.input, "assertion": b.assertion}},
		Questions: typesafe.Questions{"relationship": typesafe.Choice{
			Instructions: "Do these cases exercise the same behavior or different behaviors, accounting for boundary differences?",
			Criteria:     typesafe.ChoiceCriteria{"the_same_behavior": "Same input class and outcome", "different_behaviors": "Different behavior or meaningful boundary", "insufficient_context": "Cannot tell from summaries"},
		}},
	})
	if err != nil {
		fmt.Println("INCONCLUSIVE: retain both tests;", err)
		return
	}
	answer := resp.Choices()["relationship"]
	if answer.Confidence < 0.85 || answer.Choice == "insufficient_context" {
		fmt.Printf("INCONCLUSIVE: retain both tests (%s, %.2f)\n", answer.Choice, answer.Confidence)
		return
	}
	fmt.Printf("REVIEW %s / %s: %s (%.2f); retain both pending human decision\n", a.name, b.name, answer.Choice, answer.Confidence)
}
