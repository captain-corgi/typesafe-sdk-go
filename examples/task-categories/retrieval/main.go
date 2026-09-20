// Command retrieval demonstrates the Retrieval task category: a workflow
// (here, RAG) needs relevant context. Passages are graded for relevance to
// the query in one call each, then a plain Go reduce packs the best
// passages into a fixed word budget — the context block a generator would
// receive.
//
// It mirrors the "Retrieval" row of the docs use-case map's example task
// categories
// (https://docs.typesafe.ai/concepts/use-case-map#example-task-categories).
//
// Requires TYPESAFE_API_KEY; run with:
//
//	go run ./examples/task-categories/retrieval
package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/captain-corgi/typesafe-sdk-go"
)

// contextBudgetWords caps the assembled context block.
const contextBudgetWords = 60

type passage struct {
	id   string
	text string
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

	query := "How do I rotate a leaked personal access token?"
	corpus := []passage{
		{"kb-201", "Personal access tokens are rotated from Settings → Developer → Tokens. " +
			"Create the replacement first, update your scripts, then revoke the old token."},
		{"kb-205", "A token shown in a public channel, screenshot, or log line must be treated " +
			"as leaked: revoke it immediately and audit its recent use in the activity log."},
		{"kb-212", "API rate limits are per token, not per user; rotating a token resets the " +
			"burst window and does not change your plan's quota."},
		{"kb-219", "Service accounts use the same token mechanism but require an owner and a " +
			"quarterly access review by the security team."},
	}

	type scored struct {
		passage passage
		score   float64
	}
	scoredPassages := make([]scored, 0, len(corpus))
	for _, p := range corpus {
		resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
			State: map[string]any{"query": query, "passage": p.text, "passage_id": p.id},
			Questions: typesafe.Questions{
				"relevance": typesafe.Score{
					Instructions: "How well does the passage serve as context for answering the query?",
					Criteria:     typesafe.ScoreCriteria{"irrelevant", "background", "on point"},
				},
			},
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		s := resp.Scores()["relevance"].Score
		scoredPassages = append(scoredPassages, scored{passage: p, score: s})
		fmt.Printf("%s: relevance %.2f\n", p.id, s)
	}

	// Best-first greedy packing under a word budget.
	slices.SortStableFunc(scoredPassages, func(a, b scored) int { return cmp.Compare(b.score, a.score) })
	words := 0
	var context []string
	for _, sp := range scoredPassages {
		if sp.score < 1.0 {
			continue // background or irrelevant never makes the cut
		}
		n := len(strings.Fields(sp.passage.text))
		if words+n > contextBudgetWords {
			continue
		}
		words += n
		context = append(context, sp.passage.text)
	}

	fmt.Println()
	fmt.Println("assembled context for the generator:")
	for _, c := range context {
		fmt.Printf("  - %s\n", c)
	}
	fmt.Printf("budget: %d/%d words\n", words, contextBudgetWords)
}
