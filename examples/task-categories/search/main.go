// Command search demonstrates the Search task category: find the items
// that match a natural-language query. Each product gets one Noul
// "satisfies the query?" decision with explicit criteria, and a threshold
// turns the probabilities into a matched set — membership, not order
// (ordering is the Ranking category; see ../ranking).
//
// It mirrors the "Search" row of the docs use-case map's example task
// categories
// (https://docs.typesafe.ai/concepts/use-case-map#example-task-categories).
//
// Requires TYPESAFE_API_KEY; run with:
//
//	go run ./examples/task-categories/search
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/captain-corgi/typesafe-sdk-go"
)

// matchAbove is the probability an item needs to count as a hit.
const matchAbove = 0.60

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

	query := "over-ear headphones with active noise canceling under $200 and 30+ hour battery"
	items := []string{
		"SilentPro X2 over-ear, ANC, $189, 38-hour battery",
		"StudioMonitor 5 open-back reference cans, $320, wired",
		"AirFly Buds in-ear, ANC, $149, 8-hour battery (24 with case)",
		"QuietMax Over-Ear, ANC, $159, 32-hour battery, multipoint",
	}

	var matched []string
	for _, item := range items {
		p, err := match(client, query, item)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		verdict := "no"
		if p >= matchAbove {
			verdict = "MATCH"
			matched = append(matched, item)
		}
		fmt.Printf("p(match)=%.2f %-6s %s\n", p, verdict, item)
	}

	fmt.Println()
	fmt.Printf("query: %s\n", query)
	fmt.Printf("matched %d of %d items:\n", len(matched), len(items))
	for _, item := range matched {
		fmt.Println("  -", item)
	}
}

// match asks one Noul per item: does the item satisfy every constraint in
// the query?
func match(client *typesafe.Client, query, item string) (float64, error) {
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: map[string]any{
			"query": query,
			"item":  item,
		},
		Questions: typesafe.Questions{
			"matches": typesafe.Noul{
				Instructions: "Does the item satisfy every constraint in the query?",
				Criteria: &typesafe.NoulCriteria{
					True:  "Meets all stated requirements (type, features, price, battery, ...)",
					False: "Misses at least one stated requirement",
				},
			},
		},
	})
	if err != nil {
		return 0, err
	}
	return resp.Nouls()["matches"].Noul, nil
}
