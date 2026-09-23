// Command check-generated-output screens a draft before returning it to a user.
// Requires TYPESAFE_API_KEY; run with: go run ./examples/go-software-use-cases/16-check-generated-output
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/captain-corgi/typesafe-sdk-go"
)

func main() {
	client, err := typesafe.NewClient()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer client.Close()
	draft := "Your account is guaranteed to remain online with zero outages next year."
	if len(os.Args) > 1 {
		draft = os.Args[1]
	}
	const fallback = "I cannot confirm that claim. Please consult the published service terms."
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: map[string]any{
			"draft":  draft,
			"policy": "Do not promise zero outages, reveal private information, or give advice outside service support.",
		},
		Questions: typesafe.Questions{
			"forbidden_claim": typesafe.Noul{Instructions: "Does the draft make a promise prohibited by the output policy?"},
			"private_info":    typesafe.Noul{Instructions: "Does the draft expose private user or service information?"},
			"outside_scope":   typesafe.Noul{Instructions: "Does the draft give advice outside service support?"},
			"harm": typesafe.Score{Instructions: "What is the likely harm if this draft is returned?", Criteria: typesafe.ScoreCriteria{
				"No apparent harm", "Potential confusion", "Material harm",
			}},
		},
	})
	if err != nil {
		fmt.Println("held for review; safe fallback:", fallback, "reason:", err)
		return
	}
	flags, harm := resp.Nouls(), resp.Scores()["harm"]
	if harm.Confidence < 0.70 {
		fmt.Println("held for review; safe fallback:", fallback)
		return
	}
	if harm.Score >= 1.5 && (flags["forbidden_claim"].Noul >= 0.80 || flags["private_info"].Noul >= 0.80 || flags["outside_scope"].Noul >= 0.80) {
		fmt.Println("replaced with fixed fallback:", fallback)
		return
	}
	if harm.Score <= 0.5 && flags["forbidden_claim"].Noul <= 0.20 && flags["private_info"].Noul <= 0.20 && flags["outside_scope"].Noul <= 0.20 {
		fmt.Println("return draft:", draft)
		return
	}
	fmt.Println("held for review; safe fallback:", fallback)
}
