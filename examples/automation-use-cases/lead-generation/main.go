// Command lead-generation demonstrates composite scoring: several Score
// questions about a structured lead, merged into a weighted composite and
// cut into priority buckets in plain Go.
//
// It mirrors the docs "Pattern: composite scoring" page and the Lead
// Generation entry of the use-case map's example automation use cases.
//
// Requires TYPESAFE_API_KEY; run with:
//
//	go run ./examples/automation-use-cases/lead-generation
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

// weights for the composite; each sub-score is normalized to [0, 1] first.
var weights = map[string]float64{
	"icp_fit":    0.5,
	"maturity":   0.3,
	"buy_signal": 0.2,
}

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

	leads := []map[string]any{
		{
			"company": map[string]any{
				"name":      "Acme Rockets",
				"industry":  "aerospace",
				"employees": 120,
				"arr_usd":   4_000_000,
				"stack":     []string{"golang", "kubernetes", "postgres"},
			},
			"inbound_message": "We need to evaluate a solution for our support triage " +
				"before Q4 budget closes. Our team evaluated two vendors already. " +
				"Can you arrange a security review with our infra team next week?",
		},
		{
			"company": map[string]any{
				"name":      "Sunset Bakery",
				"industry":  "food service",
				"employees": 8,
				"arr_usd":   120_000,
				"stack":     []string{"spreadsheets"},
			},
			"inbound_message": "Hi! Just browsing, what does this do? Is there a free tier?",
		},
	}

	for _, lead := range leads {
		fmt.Println("lead:", lead["company"].(map[string]any)["name"])
		score(client, lead)
		fmt.Println()
	}
}

func score(client *typesafe.Client, lead map[string]any) {
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: lead,
		Questions: typesafe.Questions{
			"icp_fit": typesafe.Score{
				Instructions: "How well does this company match our ideal customer profile " +
					"(B2B SaaS, 50+ employees, cloud-native stack)?",
				Criteria: typesafe.ScoreCriteria{"no fit", "partial fit", "strong fit"},
			},
			"maturity": typesafe.Score{
				Instructions: "How mature is their buying process?",
				Criteria:     typesafe.ScoreCriteria{"exploring", "evaluating vendors", "ready to buy"},
			},
			"buy_signal": typesafe.Score{
				Instructions: "How strong are the buying signals in the message?",
				Criteria:     typesafe.ScoreCriteria{"curiosity", "interest", "urgent need"},
			},
		},
	})
	if err != nil {
		fmt.Println("  error:", err)
		return
	}

	composite := 0.0
	for _, name := range sortedNames(weights) {
		weight := weights[name]
		answer := resp.Scores()[name]
		max := float64(len(answer.Legend) - 1)
		normalized := answer.Score / max
		composite += weight * normalized
		fmt.Printf("  %s: %.2f/%d (confidence %.2f)\n", name, answer.Score, len(answer.Legend)-1, answer.Confidence)
	}
	fmt.Printf("  composite: %.2f\n", composite)

	switch {
	case composite >= 0.7:
		fmt.Println("  bucket: hot — route to sales today")
	case composite >= 0.4:
		fmt.Println("  bucket: warm — add to nurture sequence")
	default:
		fmt.Println("  bucket: cold — self-serve drip")
	}
}

// sortedNames keeps the printed output deterministic.
func sortedNames(m map[string]float64) []string {
	names := slices.Sorted(maps.Keys(m))
	return names
}
