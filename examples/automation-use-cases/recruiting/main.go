// Command recruiting demonstrates candidate evaluation: a resume checked
// against a role's competency rubric and hard requirements in one call,
// scored into a plain Go decision that is compared with the model's own
// recommendation — taking the conservative side when they disagree.
//
// It mirrors the "Recruiting" entry of the docs use-case map's example
// automation use cases
// (https://docs.typesafe.ai/concepts/use-case-map#example-automation-use-cases).
//
// Requires TYPESAFE_API_KEY; run with:
//
//	go run ./examples/automation-use-cases/recruiting
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/captain-corgi/typesafe-sdk-go"
)

// weights for the competency composite; each Score is normalized to [0, 1].
var weights = map[string]float64{
	"backend_depth": 0.6,
	"design_skills": 0.4,
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

	state := map[string]any{
		"role": map[string]any{
			"title":         "Senior Backend Engineer",
			"hard_required": []string{"6+ years professional experience", "fintech or payments domain"},
			"competencies":  []string{"backend depth", "system design"},
			"score_bar":     0.6,
		},
		"resume": map[string]any{
			"summary": "Backend engineer, 8 years across two payments startups. Led the ledger " +
				"redesign at PayFlow (double-entry, idempotent transfers, 4-person team). " +
				"Mostly Go and Postgres; some Kubernetes.",
			"recent_title": "Staff Engineer, PayFlow",
		},
	}

	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: state,
		Questions: typesafe.Questions{
			"backend_depth": typesafe.Score{
				Instructions: "Rate the depth of backend engineering evidence in the resume.",
				Criteria:     typesafe.ScoreCriteria{"no evidence", "working knowledge", "deep expertise"},
			},
			"design_skills": typesafe.Score{
				Instructions: "Rate the evidence of leading system design.",
				Criteria:     typesafe.ScoreCriteria{"no evidence", "familiar", "leads design"},
			},
			"meets_experience_bar": typesafe.Noul{
				Instructions: "Does the resume evidence 6+ years of professional software experience?",
				Criteria: &typesafe.NoulCriteria{
					True:  "Dates, roles, or a statement that clearly cover six or more years",
					False: "Junior tenure, unclear dates, or less than six years",
				},
			},
			"has_domain": typesafe.Noul{
				Instructions: "Does the resume show fintech or payments domain experience?",
			},
			"recommendation": typesafe.Choice{
				Instructions: "What is the fair next step for this candidate?",
				Criteria: typesafe.ChoiceCriteria{
					"advance to onsite": "Strong on competencies and hard requirements",
					"phone screen":      "Promising but with gaps to probe",
					"decline":           "Misses hard requirements or the bar",
				},
			},
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	composite := 0.0
	for _, name := range []string{"backend_depth", "design_skills"} {
		answer := resp.Scores()[name]
		normalized := answer.Score / float64(len(answer.Legend)-1)
		composite += weights[name] * normalized
		fmt.Printf("%s: %.2f/%d (confidence %.2f)\n", name, answer.Score, len(answer.Legend)-1, answer.Confidence)
	}
	experience := resp.Nouls()["meets_experience_bar"].Noul
	domain := resp.Nouls()["has_domain"].Noul
	modelRec := resp.Choices()["recommendation"]
	fmt.Printf("composite: %.2f (bar 0.60)\n", composite)
	fmt.Printf("meets experience bar: %.2f\n", experience)
	fmt.Printf("has domain: %.2f\n", domain)
	fmt.Printf("model recommendation: %s (confidence %.2f)\n", modelRec.Choice, modelRec.Confidence)

	rubric := rubricDecision(composite, experience, domain)
	fmt.Println("rubric decision:", rubric)
	fmt.Println("final decision:", conservative(rubric, modelRec.Choice))
}

// rubricDecision applies the role's bar in plain Go.
func rubricDecision(composite, experience, domain float64) string {
	switch {
	case experience < 0.5 || domain < 0.5:
		return "decline (misses a hard requirement)"
	case composite >= 0.6:
		return "advance to onsite"
	default:
		return "phone screen"
	}
}

// conservative never advances past what both the rubric and the model
// support; anything stricter wins.
func conservative(rubric, model string) string {
	if rubric == "advance to onsite" && model == "advance to onsite" {
		return "advance to onsite"
	}
	if rubric == "decline" || model == "decline" {
		return "decline"
	}
	return "phone screen"
}
