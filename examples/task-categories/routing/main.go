// Command routing demonstrates intent routing with confidence-gated
// branching: classify a voice-banking utterance, then let the choice
// confidence decide between auto-action, confirmation, and human handoff.
//
// It mirrors the docs "Patterns: intent routing" and "confidence-gated
// routing" pages and the Routing entry of the use-case map's example task
// categories.
//
// Requires TYPESAFE_API_KEY; run with:
//
//	go run ./examples/task-categories/routing
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/captain-corgi/typesafe-sdk-go"
)

// thresholds for the confidence gates.
const (
	autoActThreshold    = 0.85
	humanAgentThreshold = 0.60
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

	utterances := []string{
		"Hey, can you check what my balance is right now?",
		"Please approve the transfer to my landlord, it's due today.",
		"So yeah I was wondering if maybe you could help with a thing?",
	}

	for _, utterance := range utterances {
		fmt.Println("utterance:", utterance)
		route(client, utterance)
		fmt.Println()
	}
}

func route(client *typesafe.Client, utterance string) {
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: utterance,
		Questions: typesafe.Questions{
			"intent": typesafe.Choice{
				Instructions: "What does the caller want?",
				Criteria: typesafe.ChoiceCriteria{
					"check_balance":       "Ask how much money is in an account",
					"approve_transfer":    "Confirm or authorize moving money",
					"transaction_history": "Ask about past payments or transfers",
					"other":               "Anything else, including unclear requests",
				},
			},
		},
	})
	if err != nil {
		fmt.Println("  error:", err)
		return
	}

	intent := resp.Choices()["intent"]
	fmt.Printf("  intent: %s (confidence %.2f)\n", intent.Choice, intent.Confidence)

	runner := intentRunner(intent.Choice)
	switch {
	case intent.Confidence >= autoActThreshold:
		fmt.Printf("  route: auto (%s)\n", runner)
	case intent.Confidence < humanAgentThreshold:
		fmt.Println("  route: human agent (confidence below threshold)")
	default:
		fmt.Printf("  route: ask user to confirm before running (%s)\n", runner)
	}
}

func intentRunner(intent string) string {
	switch intent {
	case "check_balance":
		return "run accounts.get_balance()"
	case "approve_transfer":
		return "run transfers.create() after confirmation"
	case "transaction_history":
		return "run transactions.list()"
	default:
		return "no automated action available"
	}
}
