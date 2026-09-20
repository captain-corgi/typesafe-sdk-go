// Command scoring demonstrates the Scoring task category: an ordered
// rubric answer. Support responses are graded on a quality rubric and a
// resolves-the-issue Noul, and a plain Go rule picks the ticket's next
// step (close, follow up, or reassign and coach).
//
// It mirrors the "Scoring" row of the docs use-case map's example task
// categories
// (https://docs.typesafe.ai/concepts/use-case-map#example-task-categories).
//
// Requires TYPESAFE_API_KEY; run with:
//
//	go run ./examples/task-categories/scoring
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

	tickets := []map[string]any{
		{
			"issue": "Export button crashes the app on large reports",
			"response": "A fix for the export crash ships in version 4.2 (Thursday). Meanwhile, " +
				"splitting the report by month works around it. Ping me if Thursday's build " +
				"still crashes and I'll reopen this with engineering.",
		},
		{
			"issue": "Export button crashes the app on large reports",
			"response": "Thanks for your report! We take quality seriously and have shared it " +
				"with the team.",
		},
	}

	for i, ticket := range tickets {
		fmt.Printf("ticket %d issue: %s\n", i+1, ticket["issue"])
		fmt.Printf("  response: %s\n", ticket["response"])
		grade(client, ticket)
		fmt.Println()
	}
}

func grade(client *typesafe.Client, ticket map[string]any) {
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: ticket,
		Questions: typesafe.Questions{
			"quality": typesafe.Score{
				Instructions: "Grade the support response against the issue.",
				Criteria:     typesafe.ScoreCriteria{"unhelpful", "partial", "complete"},
			},
			"resolves_issue": typesafe.Noul{
				Instructions: "Does this response resolve the customer's issue or give a " +
					"credible path to resolution?",
				Criteria: &typesafe.NoulCriteria{
					True:  "Fix, workaround, or committed timeline that matches the issue",
					False: "Acknowledgment without substance",
				},
			},
		},
	})
	if err != nil {
		fmt.Println("  error:", err)
		return
	}

	quality := resp.Scores()["quality"]
	resolves := resp.Nouls()["resolves_issue"].Noul

	fmt.Printf("  quality: %.2f/2 (confidence %.2f)\n", quality.Score, quality.Confidence)
	for level := range quality.Legend {
		fmt.Printf("  quality p(%d %v): %.2f\n", level, quality.Legend[level], quality.Probabilities[level])
	}
	fmt.Printf("  resolves issue: %.2f\n", resolves)

	switch {
	case quality.Score >= 1.5 && resolves >= 0.5:
		fmt.Println("  next step: close the ticket, schedule CSAT survey")
	case quality.Score >= 0.5:
		fmt.Println("  next step: keep open, send follow-up")
	default:
		fmt.Println("  next step: reassign to senior support and coach the author")
	}
}
