// Command ranking demonstrates re-ranking with map-reduce: score the
// relevance of each retrieved passage to a query in separate SystemOne
// calls, sort by expected score, and reduce into a top-k result list.
//
// It mirrors the docs "Cookbook: re-ranking" page and the Ranking entry of
// the use-case map's example task categories.
//
// Requires TYPESAFE_API_KEY; run with:
//
//	go run ./examples/task-categories/ranking
package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"slices"

	"github.com/captain-corgi/typesafe-sdk-go"
)

type passage struct {
	id   string
	text string
}

type ranked struct {
	passage passage
	score   float64
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

	query := "How do I cancel my subscription and get a refund for the current month?"
	corpus := []passage{
		{"kb-001", "To cancel your subscription, go to Settings → Billing → Cancel plan. " +
			"Your access continues until the end of the paid period."},
		{"kb-014", "Refunds for the current month are prorated automatically once the " +
			"cancellation is confirmed; the amount appears on your invoice within 5 business days."},
		{"kb-023", "Our team hosts a monthly webinar about billing best practices and " +
			"invoice customization for growing teams."},
		{"kb-031", "If your card was charged twice in the same billing cycle, contact " +
			"support with the two invoice numbers for an immediate reversal."},
		{"kb-045", "Keyboard shortcuts: press ? anywhere in the app to open the shortcut cheat sheet."},
	}

	// Map: score every passage against the query.
	rankedPassages := make([]ranked, 0, len(corpus))
	for _, p := range corpus {
		score, err := relevance(client, query, p)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		rankedPassages = append(rankedPassages, ranked{passage: p, score: score})
	}

	// Reduce: sort by relevance (expected score) descending, keep the top 3.
	slices.SortStableFunc(rankedPassages, func(a, b ranked) int { return cmp.Compare(b.score, a.score) })
	for position, entry := range rankedPassages[:3] {
		fmt.Printf("%d. %s (relevance %.2f)\n   %s\n", position+1, entry.passage.id, entry.score, entry.passage.text)
	}
}

// relevance asks one score question about the query-passage pair.
func relevance(client *typesafe.Client, query string, p passage) (float64, error) {
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: map[string]any{
			"query":      query,
			"passage":    p.text,
			"passage_id": p.id,
		},
		Questions: typesafe.Questions{
			"relevance": typesafe.Score{
				Instructions: "Does the passage answer the query?",
				Criteria:     typesafe.ScoreCriteria{"irrelevant", "related but incomplete", "directly answers"},
			},
		},
	})
	if err != nil {
		return 0, err
	}
	return resp.Scores()["relevance"].Score, nil
}
