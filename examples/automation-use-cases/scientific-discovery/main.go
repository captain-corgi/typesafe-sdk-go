// Command scientific-discovery demonstrates literature screening: paper
// abstracts evaluated against systematic-review inclusion and exclusion
// criteria in one call each, plus a reporting-quality check, with a plain
// Go verdict of include, exclude, or needs review.
//
// It mirrors the "Scientific discovery" entry of the docs use-case map's
// example automation use cases
// (https://docs.typesafe.ai/concepts/use-case-map#example-automation-use-cases).
//
// Requires TYPESAFE_API_KEY; run with:
//
//	go run ./examples/automation-use-cases/scientific-discovery
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/captain-corgi/typesafe-sdk-go"
)

type screening struct {
	meetsInclusion  float64
	violatesExclude float64
	missingMethods  float64
	relevance       float64
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

	papers := []map[string]any{
		{
			"title": "Slow-wave sleep strengthens declarative memory in older adults",
			"abstract": "Randomized crossover trial, N=64 adults aged 60-75. Targeted slow-wave " +
				"auditory stimulation overnight improved next-morning word-pair recall by 14% " +
				"versus sham (p<0.01, mixed-effects model). Pre-registered on OSF.",
		},
		{
			"title": "Sleep and cognition: a narrative synthesis",
			"abstract": "We review two decades of literature on sleep and memory and propose an " +
				"integrative framework connecting consolidation research with clinical practice.",
		},
	}

	for _, paper := range papers {
		fmt.Println("paper:", paper["title"])
		s, err := screen(client, paper)
		if err != nil {
			fmt.Println("  error:", err)
			fmt.Println()
			continue
		}
		fmt.Printf("  %-19s %.2f\n", "meets inclusion:", s.meetsInclusion)
		fmt.Printf("  %-19s %.2f\n", "violates exclusion:", s.violatesExclude)
		fmt.Printf("  %-19s %.2f\n", "missing methodology:", s.missingMethods)
		fmt.Printf("  %-19s %.2f\n", "relevance:", s.relevance)
		fmt.Println("  verdict:", s.verdict())
		fmt.Println()
	}
}

func screen(client *typesafe.Client, paper map[string]any) (screening, error) {
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: paper,
		Questions: typesafe.Questions{
			"meets_inclusion": typesafe.Noul{
				Instructions: "Does this study meet the review's inclusion criteria?",
				Criteria: &typesafe.NoulCriteria{
					True:  "Empirical study of sleep and memory in human adults, with a measured outcome",
					False: "Review, opinion, animal study, or unrelated topic",
				},
			},
			"violates_exclusion": typesafe.Noul{
				Instructions: "Does this study hit any exclusion criterion?",
				Criteria: &typesafe.NoulCriteria{
					True:  "Non-randomized design, sample under 30, or population outside 18-80 years",
					False: "Design, sample, and population are all within limits",
				},
			},
			"missing_methodology": typesafe.Noul{
				Instructions: "Does the abstract omit key methodological details " +
					"(sample size, design, or outcome measure)?",
			},
			"relevance": typesafe.Score{
				Instructions: "How relevant is this study to the review question " +
					"(does sleep affect memory consolidation)?",
				Criteria: typesafe.ScoreCriteria{"tangential", "related", "on-topic"},
			},
		},
	})
	if err != nil {
		return screening{}, err
	}
	return screening{
		meetsInclusion:  resp.Nouls()["meets_inclusion"].Noul,
		violatesExclude: resp.Nouls()["violates_exclusion"].Noul,
		missingMethods:  resp.Nouls()["missing_methodology"].Noul,
		relevance:       resp.Scores()["relevance"].Score,
	}, nil
}

// verdict applies the screening decision rules in plain Go.
func (s screening) verdict() string {
	switch {
	case s.violatesExclude >= 0.5:
		return "exclude (hits an exclusion criterion)"
	case s.meetsInclusion < 0.5:
		return "exclude (does not meet inclusion criteria)"
	case s.relevance >= 1.0 && s.missingMethods < 0.5:
		return "include"
	default:
		return "needs review (on-topic but incomplete reporting)"
	}
}
