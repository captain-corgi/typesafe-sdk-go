// Command predictive-features demonstrates feature extraction for
// predictive modeling: free-text reviews turned into a deterministic
// feature vector of Noul probabilities plus a normalized sentiment Score —
// the kind of signal a classical ML model (churn, propensity, forecast)
// can train on directly.
//
// It mirrors the "Feature extraction for predictive modeling" entry of the
// docs use-case map's example automation use cases
// (https://docs.typesafe.ai/concepts/use-case-map#example-automation-use-cases).
//
// Requires TYPESAFE_API_KEY; run with:
//
//	go run ./examples/automation-use-cases/predictive-features
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
		"The dashboard is great but we're evaluating a cheaper competitor for next renewal.",
		"Onboarding was smooth and the team is already asking for more seats.",
	}

	for _, review := range reviews {
		fmt.Println("review:", review)
		features, err := extract(client, review)
		if err != nil {
			fmt.Println("  error:", err)
			fmt.Println()
			continue
		}
		for _, name := range sortedNames(features) {
			fmt.Printf("  %-22s %.2f\n", name+" =", features[name])
		}
		fmt.Println("  feature row:", featureRow(features))
		fmt.Println()
	}
}

// extract turns one review into a flat feature map, all values in [0, 1].
func extract(client *typesafe.Client, review string) (map[string]float64, error) {
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: review,
		Questions: typesafe.Questions{
			"purchase_intent": typesafe.Noul{
				Instructions: "Does this text signal intent to buy, expand, or renew?",
			},
			"urgency": typesafe.Noul{
				Instructions: "Does this text signal urgency (deadlines, renewals, executive asks)?",
			},
			"competitor_mention": typesafe.Noul{
				Instructions: "Does this text mention evaluating or switching to a competitor?",
			},
			"sentiment": typesafe.Score{
				Instructions: "What is the overall sentiment toward the product?",
				Criteria:     typesafe.ScoreCriteria{"negative", "neutral", "positive"},
			},
		},
	})
	if err != nil {
		return nil, err
	}
	sentiment := resp.Scores()["sentiment"]
	return map[string]float64{
		"purchase_intent":    resp.Nouls()["purchase_intent"].Noul,
		"urgency":            resp.Nouls()["urgency"].Noul,
		"competitor_mention": resp.Nouls()["competitor_mention"].Noul,
		"sentiment":          sentiment.Score / float64(len(sentiment.Legend)-1),
	}, nil
}

// featureRow renders the map as one deterministic line of name=value pairs,
// the shape a downstream model or feature store would ingest.
func featureRow(features map[string]float64) string {
	cols := make([]string, 0, len(features))
	for _, name := range sortedNames(features) {
		cols = append(cols, fmt.Sprintf("%s=%.2f", name, features[name]))
	}
	return strings.Join(cols, ",")
}

// sortedNames keeps the feature order deterministic.
func sortedNames(m map[string]float64) []string {
	names := slices.Sorted(maps.Keys(m))
	return names
}
