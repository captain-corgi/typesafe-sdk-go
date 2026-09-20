// Command structured-data-extraction demonstrates the Structured Data
// Extraction task category: recovering known fields from unstructured
// input. A bio is normalized into a typed record — enum fields as Choices,
// boolean fields as Nouls, a bucketed field as a Score — and low
// confidence flags the field for human review instead of trusting a guess.
//
// It mirrors the "Structured Data Extraction" row of the docs use-case
// map's example task categories
// (https://docs.typesafe.ai/concepts/use-case-map#example-task-categories).
//
// Requires TYPESAFE_API_KEY; run with:
//
//	go run ./examples/task-categories/structured-data-extraction
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/captain-corgi/typesafe-sdk-go"
)

// reviewBelow flags a field for human confirmation instead of trusting it.
const reviewBelow = 0.60

const bio = "Maya Okonkwo — shipped design systems at two fintechs, now leading a " +
	"platform team of nine. Avid climber, remote from Lisbon since 2022, speaks at " +
	"Go and UX meetups. Started out as a self-taught developer around a decade ago."

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

	fmt.Println("input bio:", bio)
	fmt.Println()

	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: bio,
		Questions: typesafe.Questions{
			"job_family": typesafe.Choice{
				Instructions: "Which job family does this person primarily work in?",
				Criteria: typesafe.ChoiceCriteria{
					"engineering": "Builds or operates software",
					"design":      "Designs products or systems",
					"product":     "Owns roadmap and outcomes",
					"marketing":   "Growth, content, or brand",
				},
			},
			"seniority": typesafe.Choice{
				Instructions: "Which seniority band fits best?",
				Criteria: typesafe.ChoiceCriteria{
					"junior":     "Learning the craft",
					"mid":        "Independent contributor",
					"senior":     "Owns significant scope",
					"leadership": "Leads people or a function",
				},
			},
			"works_remotely": typesafe.Noul{
				Instructions: "Does the bio say the person works remotely?",
			},
			"leads_team": typesafe.Noul{
				Instructions: "Does the bio say the person leads a team?",
			},
			"experience_band": typesafe.Score{
				Instructions: "How many years of professional experience does the bio indicate?",
				Criteria:     typesafe.ScoreCriteria{"under 3", "3-5", "6-9", "10 or more"},
			},
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	jobFamily := resp.Choices()["job_family"]
	seniority := resp.Choices()["seniority"]
	remote := resp.Nouls()["works_remotely"].Noul
	leads := resp.Nouls()["leads_team"].Noul
	experience := resp.Scores()["experience_band"]

	// Buckets come back as an index into the rubric; round to nearest.
	bucket := int(experience.Score + 0.5)
	if bucket > len(experience.Legend)-1 {
		bucket = len(experience.Legend) - 1
	}

	fmt.Println("extracted record:")
	printField("job_family", jobFamily.Choice, jobFamily.Confidence)
	printField("seniority", seniority.Choice, seniority.Confidence)
	printField("works_remotely", fmt.Sprintf("%.2f (p true)", remote), 1)
	printField("leads_team", fmt.Sprintf("%.2f (p true)", leads), 1)
	printField("experience_band", fmt.Sprintf("%v", experience.Legend[bucket]), experience.Confidence)
}

// printField appends a review flag whenever confidence is below the bar.
func printField(name, value string, confidence float64) {
	note := ""
	if confidence < reviewBelow {
		note = "  [needs review]"
	}
	fmt.Printf("  %-16s %s%s\n", name+":", value, note)
}
