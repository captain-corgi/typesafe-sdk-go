// Command model-routing demonstrates a custom LLM router: every prompt is
// classified by domain, difficulty, and reasoning need, and a plain Go
// routing table picks the cheapest model that can handle it — with a
// confidence gate that falls back to the standard tier.
//
// It mirrors the "Model routing" entry of the docs use-case map's example
// automation use cases
// (https://docs.typesafe.ai/concepts/use-case-map#example-automation-use-cases).
//
// Requires TYPESAFE_API_KEY; run with:
//
//	go run ./examples/automation-use-cases/model-routing
package main

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"

	"github.com/captain-corgi/typesafe-sdk-go"
)

// confidenceBelow is the routing-table confidence gate.
const confidenceBelow = 0.60

func main() {
	client, err := typesafe.NewClient()
	if err != nil {
		if errors.Is(err, typesafe.ErrMissingAPIKey) {
			fmt.Fprintln(os.Stderr, "Set TYPESAFE_API_KEY before running this example.")
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	defer client.Close()

	prompts := []string{
		"Rewrite this haiku in the style of a pirate: silent frost on windows",
		"Our checkout conversion dropped 12% after the redesign. Here are the funnel " +
			"numbers per step. What should we investigate first?",
		"Write a PostgreSQL query that returns each user's third order by created_at.",
	}

	for _, prompt := range prompts {
		fmt.Println("prompt:", prompt)
		route(client, prompt)
		fmt.Println()
	}
}

func route(client *typesafe.Client, prompt string) {
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: prompt,
		Questions: typesafe.Questions{
			"domain": typesafe.Choice{
				Instructions: "What kind of task is this prompt?",
				Criteria: typesafe.ChoiceCriteria{
					"creative": "Writing, style, or brainstorming",
					"analysis": "Reasoning over data, debugging, or planning",
					"lookup":   "A factual answer or a well-known snippet",
					"support":  "Customer-facing help or etiquette",
				},
			},
			"difficulty": typesafe.Score{
				Instructions: "How hard is this prompt to answer well?",
				Criteria:     typesafe.ScoreCriteria{"trivial", "standard", "hard"},
			},
			"needs_reasoning": typesafe.Noul{
				Instructions: "Does answering well require multi-step reasoning, math, or careful planning?",
				Criteria: &typesafe.NoulCriteria{
					True:  "Needs deduction, calculation, or a plan across several steps",
					False: "A single straightforward reply suffices",
				},
			},
		},
	})
	if err != nil {
		fmt.Println("  error:", err)
		return
	}

	domain := resp.Choices()["domain"]
	difficulty := resp.Scores()["difficulty"].Score
	reasoning := resp.Nouls()["needs_reasoning"].Noul

	fmt.Printf("  domain: %s (confidence %.2f)\n", domain.Choice, domain.Confidence)
	for _, label := range sortedKeys(domain.Probabilities) {
		fmt.Printf("  domain p(%s): %.2f\n", label, domain.Probabilities[label])
	}
	fmt.Printf("  difficulty: %.2f/2\n", difficulty)
	fmt.Printf("  needs reasoning: %.2f\n", reasoning)

	switch {
	case domain.Confidence < confidenceBelow:
		fmt.Println("  route: standard (domain confidence below threshold)")
	case reasoning >= 0.5 || difficulty >= 1.5:
		fmt.Println("  route: reasoning-max (escalate to the expensive tier)")
	case difficulty < 0.5:
		fmt.Println("  route: flash-mini (cheap tier handles it)")
	default:
		fmt.Println("  route: standard")
	}
}

// sortedKeys keeps the printed probabilities deterministic.
func sortedKeys(m map[string]float64) []string {
	keys := slices.Sorted(maps.Keys(m))
	return keys
}
