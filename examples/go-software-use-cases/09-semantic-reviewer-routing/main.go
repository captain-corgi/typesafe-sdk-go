// Command semantic-reviewer-routing adds a cross-cutting reviewer to CODEOWNERS results.
// Requires TYPESAFE_API_KEY; run with: go run ./examples/go-software-use-cases/09-semantic-reviewer-routing
package main

import (
	"context"
	"fmt"
	"os"
	"slices"

	"github.com/captain-corgi/typesafe-sdk-go"
)

func main() {
	client, err := typesafe.NewClient()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer client.Close()
	changedPath := "transport.go"
	diffSummary := "A new retry branch now logs request headers before sending a second attempt."
	// Exact path ownership is resolved by CODEOWNERS (represented by this fixed result).
	reviewers := []string{"@sdk-maintainers"}
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: map[string]any{"path": changedPath, "diff_summary": diffSummary},
		Questions: typesafe.Questions{"concern": typesafe.Choice{
			Instructions: "Which additional cross-cutting concern is most relevant?",
			Criteria: typesafe.ChoiceCriteria{
				"api_contract": "Public API or wire behavior", "security_boundary": "Secrets, authentication, or trust boundary",
				"performance": "Latency or resource use", "observability": "Logs, metrics, or tracing", "other": "No clear concern",
			},
		}},
	})
	if err != nil {
		fmt.Println("Semantic routing unavailable; keep CODEOWNERS reviewers:", err)
		fmt.Println(reviewers)
		return
	}
	concern := resp.Choices()["concern"]
	additional := map[string]string{
		"api_contract": "@api-reviewers", "security_boundary": "@security-reviewers",
		"performance": "@performance-reviewers", "observability": "@observability-reviewers",
	}
	if reviewer, ok := additional[concern.Choice]; ok && concern.Confidence >= 0.70 && !slices.Contains(reviewers, reviewer) {
		reviewers = append(reviewers, reviewer)
	} else if concern.Confidence < 0.70 {
		reviewers = append(reviewers, "@triage-reviewers")
	}
	fmt.Printf("%s reviewers: %v\n", changedPath, reviewers)
}
