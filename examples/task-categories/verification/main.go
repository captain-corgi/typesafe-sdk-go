// Command verification demonstrates the Verification task category:
// checking an artifact for failure modes. Each claim is checked against
// its cited passage — does the citation support the claim, how strongly,
// and is the claim a misquote? — and a plain Go rule verifies it or sends
// it to citation review.
//
// It mirrors the "Verification" row of the docs use-case map's example
// task categories
// (https://docs.typesafe.ai/concepts/use-case-map#example-task-categories).
//
// Requires TYPESAFE_API_KEY; run with:
//
//	go run ./examples/task-categories/verification
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/captain-corgi/typesafe-sdk-go"
)

type citation struct {
	claim   string
	passage string
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

	citations := []citation{
		{
			claim: "The study found a 14% improvement in word-pair recall from targeted " +
				"slow-wave stimulation.",
			passage: "Targeted slow-wave auditory stimulation overnight improved " +
				"next-morning word-pair recall by 14% versus sham (p<0.01).",
		},
		{
			claim: "The study proved sleep stimulation cures memory loss in everyone.",
			passage: "Targeted slow-wave auditory stimulation overnight improved " +
				"next-morning word-pair recall by 14% versus sham (p<0.01).",
		},
	}

	for _, c := range citations {
		fmt.Println("claim:", c.claim)
		fmt.Println("  cited passage:", c.passage)
		verify(client, c)
		fmt.Println()
	}
}

func verify(client *typesafe.Client, c citation) {
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: map[string]any{
			"claim":    c.claim,
			"citation": c.passage,
		},
		Questions: typesafe.Questions{
			"supported": typesafe.Noul{
				Instructions: "Does the cited passage support the claim?",
				Criteria: &typesafe.NoulCriteria{
					True:  "The passage states what the claim asserts",
					False: "The passage is unrelated to the claim",
				},
			},
			"support_strength": typesafe.Score{
				Instructions: "How strongly does the passage bear on the claim?",
				Criteria: typesafe.ScoreCriteria{
					"contradicts", "unrelated", "weak support", "direct support",
				},
			},
			"overstates": typesafe.Noul{
				Instructions: "Does the claim overstate or generalize beyond what the passage says?",
			},
		},
	})
	if err != nil {
		fmt.Println("  error:", err)
		return
	}

	supported := resp.Nouls()["supported"].Noul
	strength := resp.Scores()["support_strength"]
	overstates := resp.Nouls()["overstates"].Noul

	fmt.Printf("  supported: %.2f\n", supported)
	fmt.Printf("  support strength: %.2f/3 (confidence %.2f)\n", strength.Score, strength.Confidence)
	fmt.Printf("  overstates: %.2f\n", overstates)

	switch {
	case strength.Score < 0.5:
		fmt.Println("  verdict: FAIL — citation contradicts the claim")
	case supported >= 0.5 && strength.Score >= 2.5 && overstates < 0.5:
		fmt.Println("  verdict: verified")
	default:
		fmt.Println("  verdict: citation review — support is weak or the claim overstates")
	}
}
