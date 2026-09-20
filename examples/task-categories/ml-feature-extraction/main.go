// Command ml-feature-extraction demonstrates the ML Feature Extraction
// task category: a downstream classical model needs semantic signals. Each
// review becomes a feature block — Noul probabilities plus a normalized
// sentiment Score — printed and joined into one deterministic feature row
// a model or feature store can ingest without an LLM in the serving path.
//
// It mirrors the "ML Feature Extraction" row of the docs use-case map's
// example task categories
// (https://docs.typesafe.ai/concepts/use-case-map#example-task-categories).
//
// Requires TYPESAFE_API_KEY; run with:
//
//	go run ./examples/task-categories/ml-feature-extraction
package main

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

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

	reviews := []string{
		"Bought three as gifts after trying one — the battery life is unreal.",
		"Second one that arrived dead. Refund please.",
	}

	for _, review := range reviews {
		fmt.Println("review:", review)
		features, err := features(client, review)
		if err != nil {
			fmt.Println("  error:", err)
			fmt.Println()
			continue
		}
		for _, name := range sortedNames(features) {
			fmt.Printf("  %-16s %.2f\n", name, features[name])
		}
		fmt.Println("  model input:", strings.Join(featureCols(features), ","))
		fmt.Println()
	}
}

// features maps one review onto the model's feature schema, all in [0, 1].
func features(client *typesafe.Client, review string) (map[string]float64, error) {
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: review,
		Questions: typesafe.Questions{
			"repurchase_signal": typesafe.Noul{
				Instructions: "Does this review signal repeat purchase or gifting?",
			},
			"defect_signal": typesafe.Noul{
				Instructions: "Does this review report a defect or product failure?",
			},
			"refund_request": typesafe.Noul{
				Instructions: "Is the reviewer asking for a refund or return?",
			},
			"sentiment": typesafe.Score{
				Instructions: "Rate the review's sentiment toward the product.",
				Criteria:     typesafe.ScoreCriteria{"negative", "mixed", "positive"},
			},
		},
	})
	if err != nil {
		return nil, err
	}
	sentiment := resp.Scores()["sentiment"]
	return map[string]float64{
		"repurchase_signal": resp.Nouls()["repurchase_signal"].Noul,
		"defect_signal":     resp.Nouls()["defect_signal"].Noul,
		"refund_request":    resp.Nouls()["refund_request"].Noul,
		"sentiment":         sentiment.Score / float64(len(sentiment.Legend)-1),
	}, nil
}

// featureCols renders the map as name=value in sorted order — one stable
// row per review, ready for a training corpus.
func featureCols(features map[string]float64) []string {
	cols := make([]string, 0, len(features))
	for _, name := range sortedNames(features) {
		cols = append(cols, fmt.Sprintf("%s=%.2f", name, features[name]))
	}
	return cols
}

// sortedNames keeps the feature schema order deterministic.
func sortedNames(m map[string]float64) []string {
	names := slices.Sorted(maps.Keys(m))
	return names
}
