// Command quickstart is the minimal TypeSafe AI client: one call asking all
// three question primitives about a support ticket.
//
// It mirrors the docs "Quickstart" page and covers the Customer Support
// category of the use-case map (https://docs.typesafe.ai/concepts/use-case-map).
//
// Requires TYPESAFE_API_KEY; run with:
//
//	go run ./examples/quickstart
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/captain-corgi/typesafe-sdk-go"
)

func main() {
	client, err := typesafe.NewClient() // reads TYPESAFE_API_KEY
	if err != nil {
		if errors.Is(err, typesafe.ErrMissingAPIKey) {
			fmt.Fprintln(os.Stderr, "Set TYPESAFE_API_KEY before running this example:")
			fmt.Fprintln(os.Stderr, "  export TYPESAFE_API_KEY=ts_live_...")
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	defer client.Close()

	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: "I was charged twice for my subscription this month. " +
			"Please fix this as soon as possible.",
		Questions: typesafe.Questions{
			"department": typesafe.Choice{
				Instructions: "What is this ticket about?",
				Criteria: typesafe.ChoiceCriteria{
					"billing":   "Problems with charges, invoices, or subscriptions",
					"technical": "Bugs, outages, or broken functionality",
					"sales":     "Questions before buying",
				},
			},
			"frustration": typesafe.Score{
				Instructions: "How frustrated does the customer sound?",
				Criteria:     typesafe.ScoreCriteria{"calm", "annoyed", "upset"},
			},
			"is_urgent": typesafe.Noul{Instructions: "Is this urgent?"},
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	department := resp.Choices()["department"]
	fmt.Println("department:", department.Choice)
	fmt.Printf("department confidence: %.2f\n", department.Confidence)
	for _, label := range []string{"billing", "technical", "sales"} {
		fmt.Printf("department p(%s): %.2f\n", label, department.Probabilities[label])
	}

	frustration := resp.Scores()["frustration"]
	fmt.Printf("frustration: %.2f (confidence %.2f)\n", frustration.Score, frustration.Confidence)
	for level := range frustration.Legend {
		fmt.Printf("frustration p(%d %v): %.2f\n", level, frustration.Legend[level], frustration.Probabilities[level])
	}

	fmt.Printf("is_urgent: %.2f\n", resp.Nouls()["is_urgent"].Noul)
	if resp.Usage.InputTokens != nil && resp.Usage.OutputTokens != nil {
		fmt.Printf("usage: %d input / %d output tokens\n", *resp.Usage.InputTokens, *resp.Usage.OutputTokens)
	}
	fmt.Println("request id:", resp.RequestID)
}
