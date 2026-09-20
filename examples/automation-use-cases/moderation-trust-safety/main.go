// Command moderation-trust-safety demonstrates community moderation with
// company-specific criteria: a battery of Noul checks (toxicity, harassment,
// spam, unsafe advice, personal-data exposure) plus a severity Score,
// combined into an allow / warn / review / block decision. Unlike
// llm-guardrails (which polices model inputs and outputs), this polices
// user-generated content against a trust-and-safety policy matrix.
//
// It mirrors the "Moderation / trust & safety" entry of the docs use-case
// map's example automation use cases
// (https://docs.typesafe.ai/concepts/use-case-map#example-automation-use-cases).
//
// Requires TYPESAFE_API_KEY; run with:
//
//	go run ./examples/automation-use-cases/moderation-trust-safety
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

	comments := []string{
		"Honestly the dev team should be ashamed of this update. Sloppy work, sloppy QA.",
		"Everyone report this user, their profile lists a phone number: 555-014-3398. " +
			"Also buy 5000 followers cheap, link in bio!!!",
		"First post here — the wiring diagram in the wiki fixed my amp, thanks @mira!",
	}

	for _, comment := range comments {
		fmt.Println("comment:", comment)
		moderate(client, comment)
		fmt.Println()
	}
}

func moderate(client *typesafe.Client, comment string) {
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: comment,
		Questions: typesafe.Questions{
			"toxic": typesafe.Noul{
				Instructions: "Is this comment toxic toward a person or group?",
				Criteria: &typesafe.NoulCriteria{
					True:  "Insults, dehumanizing language, or hostility aimed at someone",
					False: "Blunt or critical but not hostile",
				},
			},
			"harassment": typesafe.Noul{
				Instructions: "Does this comment harass, dogpile, or incite pile-on against a user?",
			},
			"spam": typesafe.Noul{
				Instructions: "Is this comment unsolicited promotion or engagement bait?",
			},
			"unsafe_advice": typesafe.Noul{
				Instructions: "Does this comment give advice that could cause harm if followed " +
					"(electrical, medical, financial, legal)?",
			},
			"exposes_pii": typesafe.Noul{
				Instructions: "Does this comment expose someone's personal data " +
					"(phone, address, ID, or private contact details)?",
			},
			"severity": typesafe.Score{
				Instructions: "How severe is the worst policy issue in this comment?",
				Criteria:     typesafe.ScoreCriteria{"benign", "borderline", "harmful"},
			},
		},
	})
	if err != nil {
		fmt.Println("  error:", err)
		return
	}

	signals := map[string]float64{
		"toxic":         resp.Nouls()["toxic"].Noul,
		"harassment":    resp.Nouls()["harassment"].Noul,
		"spam":          resp.Nouls()["spam"].Noul,
		"unsafe_advice": resp.Nouls()["unsafe_advice"].Noul,
		"exposes_pii":   resp.Nouls()["exposes_pii"].Noul,
	}
	severity := resp.Scores()["severity"]

	worst := 0.0
	for _, name := range sortedNames(signals) {
		value := signals[name]
		if value > worst {
			worst = value
		}
		fmt.Printf("  %-14s %.2f\n", name+":", value)
	}
	fmt.Printf("  %-14s %.2f (confidence %.2f)\n", "severity:", severity.Score, severity.Confidence)

	// Policy matrix: hard blocks for protected classes of harm, human review
	// for the severe rest, soft handling for borderline cases.
	switch {
	case signals["exposes_pii"] >= 0.5 || signals["harassment"] >= 0.7 || signals["toxic"] >= 0.7:
		fmt.Println("  action: block and log to the safety register")
	case severity.Score >= 1.5:
		fmt.Println("  action: human review within 1 hour")
	case severity.Score >= 0.5 || worst >= 0.5:
		fmt.Println("  action: warn — collapse the reply and notify the author")
	default:
		fmt.Println("  action: allow")
	}
}

// sortedNames keeps the printed signals deterministic.
func sortedNames(m map[string]float64) []string {
	names := slices.Sorted(maps.Keys(m))
	return names
}
