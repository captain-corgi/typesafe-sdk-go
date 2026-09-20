// Command insurance-claims demonstrates FNOL triage: first-notice-of-loss
// reports classified by claim type and complexity in one call, with
// missing-information and fraud-indicator checks deciding between
// straight-through processing, adjuster review, and SIU investigation.
//
// It mirrors the "Insurance claims" entry of the docs use-case map's
// example automation use cases
// (https://docs.typesafe.ai/concepts/use-case-map#example-automation-use-cases).
//
// Requires TYPESAFE_API_KEY; run with:
//
//	go run ./examples/automation-use-cases/insurance-claims
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/captain-corgi/typesafe-sdk-go"
)

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

	claims := []string{
		"Policy GL-88231. Windstorm on Tuesday lifted a branch onto my garage roof; " +
			"photos attached, one quote from a licensed roofer included. No injuries.",
		"My parked car was hit sometime last month, not sure of the date. The whole " +
			"passenger side is damaged, but the photos are from a previous accident. " +
			"This is the third similar claim I've filed this year.",
	}

	for _, fnol := range claims {
		fmt.Println("claim:", fnol)
		triage(client, fnol)
		fmt.Println()
	}
}

func triage(client *typesafe.Client, fnol string) {
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: fnol,
		Questions: typesafe.Questions{
			"claim_type": typesafe.Choice{
				Instructions: "What type of claim is this?",
				Criteria: typesafe.ChoiceCriteria{
					"collision": "Vehicle or property impact",
					"theft":     "Loss from theft or vandalism",
					"weather":   "Storm, flood, or other weather damage",
					"liability": "Third-party injury or damage claim",
				},
			},
			"complexity": typesafe.Score{
				Instructions: "How complex will this claim be to settle?",
				Criteria:     typesafe.ScoreCriteria{"simple", "moderate", "complex"},
			},
			"missing_info": typesafe.Noul{
				Instructions: "Does the report omit details an adjuster would need " +
					"(date, location, policy number, or proof of loss)?",
			},
			"fraud_indicator": typesafe.Noul{
				Instructions: "Does this report show fraud indicators?",
				Criteria: &typesafe.NoulCriteria{
					True:  "Implausible timeline, reused evidence, repeated similar claims, or inconsistent damage",
					False: "Ordinary, internally consistent loss report",
				},
			},
		},
	})
	if err != nil {
		fmt.Println("  error:", err)
		return
	}

	claimType := resp.Choices()["claim_type"]
	complexity := resp.Scores()["complexity"].Score
	missing := resp.Nouls()["missing_info"].Noul
	fraud := resp.Nouls()["fraud_indicator"].Noul

	fmt.Printf("  claim type: %s (confidence %.2f)\n", claimType.Choice, claimType.Confidence)
	fmt.Printf("  complexity: %.2f/2\n", complexity)
	fmt.Printf("  missing info: %.2f\n", missing)
	fmt.Printf("  fraud indicator: %.2f\n", fraud)

	switch {
	case fraud >= 0.5:
		fmt.Println("  action: investigate (SIU referral)")
	case complexity >= 1.5 || missing >= 0.5:
		fmt.Println("  action: review by adjuster (complex or incomplete)")
	default:
		fmt.Println("  action: straight-through processing")
	}
}
