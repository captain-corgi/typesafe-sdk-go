// Command classification demonstrates the Classification task category:
// exactly one category wins. Headlines are tagged with a topic Choice in
// one call each; the winner's confidence gates between auto-tagging and a
// human tagging queue, and the full distribution is printed sorted.
//
// It mirrors the "Classification" row of the docs use-case map's example
// task categories
// (https://docs.typesafe.ai/concepts/use-case-map#example-task-categories).
//
// Requires TYPESAFE_API_KEY; run with:
//
//	go run ./examples/task-categories/classification
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

// autoTagBelow sends low-confidence winners to the human tagging queue.
const autoTagBelow = 0.75

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

	headlines := []string{
		"Central bank holds rates steady, signals one cut before year end",
		"Underdogs stun champions in extra-time thriller",
	}

	for _, headline := range headlines {
		fmt.Println("headline:", headline)
		classify(client, headline)
		fmt.Println()
	}
}

func classify(client *typesafe.Client, headline string) {
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: headline,
		Questions: typesafe.Questions{
			"topic": typesafe.Choice{
				Instructions: "Which section does this headline belong in?",
				Criteria: typesafe.ChoiceCriteria{
					"business":      "Markets, companies, economy, or policy",
					"sports":        "Matches, athletes, leagues, or results",
					"technology":    "Software, devices, science, or research",
					"entertainment": "Film, music, celebrities, or culture",
					"world":         "Politics, conflict, or international affairs",
				},
			},
		},
	})
	if err != nil {
		fmt.Println("  error:", err)
		return
	}

	topic := resp.Choices()["topic"]
	fmt.Printf("  winner: %s (confidence %.2f)\n", topic.Choice, topic.Confidence)
	for _, label := range sortedLabels(topic.Probabilities) {
		fmt.Printf("  p(%s): %.2f\n", label, topic.Probabilities[label])
	}

	if topic.Confidence >= autoTagBelow {
		fmt.Printf("  action: auto-tag as %s\n", topic.Choice)
	} else {
		fmt.Printf("  action: queue for human tagging (confidence below %.2f)\n", autoTagBelow)
	}
}

// sortedLabels keeps the printed distribution deterministic.
func sortedLabels(m map[string]float64) []string {
	labels := slices.Sorted(maps.Keys(m))
	return labels
}
