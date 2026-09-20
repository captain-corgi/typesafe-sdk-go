// Command search-and-retrieval demonstrates semantic search feeding a RAG
// pipeline: each candidate passage gets a Noul "does it answer the query?"
// gate plus a graded relevance Score, and a plain Go reduce turns the two
// into a ranked context window — a cheap supplement or replacement for
// embedding-based retrieval.
//
// It mirrors the "Search and retrieval" entry of the docs use-case map's
// example automation use cases
// (https://docs.typesafe.ai/concepts/use-case-map#example-automation-use-cases).
//
// Requires TYPESAFE_API_KEY; run with:
//
//	go run ./examples/automation-use-cases/search-and-retrieval
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

type candidate struct {
	passage   passage
	answers   float64
	relevance float64
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

	query := "How much carry-on baggage can I bring on an international flight?"
	corpus := []passage{
		{"kb-101", "Carry-on allowance on international routes is one bag up to 8 kg " +
			"plus one personal item such as a laptop bag or handbag."},
		{"kb-117", "Checked baggage on international flights is charged per piece; the " +
			"first bag up to 23 kg is included in Flex fares."},
		{"kb-130", "Gold members of our loyalty program may bring one extra carry-on " +
			"item on every route, international flights included."},
		{"kb-142", "Musical instruments count toward your carry-on allowance and must " +
			"fit in the overhead bin or have a purchased seat."},
		{"kb-155", "Special meals on international routes can be pre-ordered up to 24 " +
			"hours before departure, including vegetarian and halal options."},
	}

	// Map: assess every passage against the query.
	candidates := make([]candidate, 0, len(corpus))
	for _, p := range corpus {
		c, err := assess(client, query, p)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		candidates = append(candidates, c)
	}

	// Reduce: keep passages that answer the query, best relevance first.
	direct := make([]candidate, 0, len(candidates))
	for _, c := range candidates {
		fmt.Printf("%s: answers %.2f, relevance %.2f\n", c.passage.id, c.answers, c.relevance)
		if c.answers >= 0.5 {
			direct = append(direct, c)
		}
	}
	slices.SortStableFunc(direct, func(a, b candidate) int { return cmp.Compare(b.relevance, a.relevance) })

	fmt.Println()
	fmt.Println("selected context (top 2 by relevance):")
	for i, c := range direct {
		if i == 2 {
			break
		}
		fmt.Printf("  %d. [%s] %s\n", i+1, c.passage.id, c.passage.text)
	}
}

// assess asks one Noul gate and one graded relevance Score per passage.
func assess(client *typesafe.Client, query string, p passage) (candidate, error) {
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: map[string]any{
			"query":      query,
			"passage":    p.text,
			"passage_id": p.id,
		},
		Questions: typesafe.Questions{
			"answers": typesafe.Noul{
				Instructions: "Does this passage answer the query?",
				Criteria: &typesafe.NoulCriteria{
					True:  "States the carry-on allowance or a rule that directly governs it",
					False: "Discusses travel without answering the query",
				},
			},
			"relevance": typesafe.Score{
				Instructions: "How relevant is the passage to the query?",
				Criteria:     typesafe.ScoreCriteria{"irrelevant", "related", "directly answers"},
			},
		},
	})
	if err != nil {
		return candidate{}, err
	}
	return candidate{
		passage:   p,
		answers:   resp.Nouls()["answers"].Noul,
		relevance: resp.Scores()["relevance"].Score,
	}, nil
}
