// Command legal-compliance demonstrates marketing-copy review: a battery
// of Noul checks for prohibited claims and missing disclaimers, plus a
// severity Score, deciding between approval, revision, and escalation to
// legal — in plain Go.
//
// It mirrors the "Legal and compliance" entry of the docs use-case map's
// example automation use cases
// (https://docs.typesafe.ai/concepts/use-case-map#example-automation-use-cases).
//
// Requires TYPESAFE_API_KEY; run with:
//
//	go run ./examples/automation-use-cases/legal-compliance
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

const adCopy = "Metaboost melts belly fat in 14 days — guaranteed! Doctors recommend it " +
	"over diet and exercise. Results shown are typical for committed users."

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

	fmt.Println("copy under review:", adCopy)
	fmt.Println()

	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: map[string]any{
			"copy":    adCopy,
			"channel": "paid social",
			"policies": []string{
				"No guaranteed outcomes",
				"No implied medical endorsement",
				"Typical-results disclaimers required on testimonial claims",
			},
		},
		Questions: typesafe.Questions{
			"guarantees_outcome": typesafe.Noul{
				Instructions: "Does the copy promise a guaranteed outcome?",
				Criteria: &typesafe.NoulCriteria{
					True:  "Uses 'guaranteed' or equivalent certainty language about results",
					False: "Outcomes are hedged or absent",
				},
			},
			"medical_claim": typesafe.Noul{
				Instructions: "Does the copy imply medical effectiveness or professional endorsement?",
			},
			"missing_disclaimer": typesafe.Noul{
				Instructions: "Does the copy lack the disclaimer its claims require?",
			},
			"severity": typesafe.Score{
				Instructions: "If any policy is violated, how severe is the worst violation?",
				Criteria:     typesafe.ScoreCriteria{"minor", "material", "severe"},
			},
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	violations := map[string]float64{
		"guarantees_outcome": resp.Nouls()["guarantees_outcome"].Noul,
		"medical_claim":      resp.Nouls()["medical_claim"].Noul,
		"missing_disclaimer": resp.Nouls()["missing_disclaimer"].Noul,
	}
	severity := resp.Scores()["severity"]

	found := 0
	for _, name := range sortedNames(violations) {
		value := violations[name]
		mark := "ok"
		if value >= 0.5 {
			mark = "VIOLATION"
			found++
		}
		fmt.Printf("%-20s %.2f %s\n", name+":", value, mark)
	}
	fmt.Printf("worst severity: %.2f/2 (confidence %.2f)\n", severity.Score, severity.Confidence)

	switch {
	case found == 0:
		fmt.Println("decision: approve for publication")
	case found > 0 && severity.Score >= 1.5:
		fmt.Println("decision: escalate to legal (material or severe violation)")
	default:
		fmt.Println("decision: request revision (minor violation)")
	}
}

// sortedNames keeps the printed findings deterministic.
func sortedNames(m map[string]float64) []string {
	names := slices.Sorted(maps.Keys(m))
	return names
}
