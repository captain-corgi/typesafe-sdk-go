// Command select-runtime-strategy combines semantic signals with Go policy constraints.
// Requires TYPESAFE_API_KEY; run with: go run ./examples/go-software-use-cases/04-select-runtime-strategy
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
	task := "Explain why checkout conversion fell after the deploy using this funnel summary."
	if len(os.Args) > 1 {
		task = os.Args[1]
	}
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: task,
		Questions: typesafe.Questions{
			"family": typesafe.Choice{Instructions: "Which task family best fits?", Criteria: typesafe.ChoiceCriteria{
				"rewrite": "Simple prose rewrite", "analysis": "Analyze evidence or debug a cause", "code": "Write or reason about code", "other": "No known family",
			}},
			"difficulty": typesafe.Score{Instructions: "How difficult is a correct answer?", Criteria: typesafe.ScoreCriteria{
				"Routine", "Needs some judgment", "Needs detailed analysis",
			}},
			"multistep": typesafe.Noul{Instructions: "Does a good answer require multiple reasoning steps?"},
		},
	})
	if err != nil {
		fmt.Println("route: standard-review (classification failed):", err)
		return
	}
	family, difficulty := resp.Choices()["family"], resp.Scores()["difficulty"]
	if family.Confidence < 0.70 || difficulty.Confidence < 0.65 || family.Choice == "other" {
		fmt.Println("route: standard-review (uncertain classification)")
		return
	}
	// Hard limits remain ordinary Go conditions, independent of the semantic result.
	const specialistCapacity = true
	const budgetAllowsSpecialist = true
	strategy := "standard"
	if family.Choice == "rewrite" && difficulty.Score < 0.7 && resp.Nouls()["multistep"].Noul < 0.3 {
		strategy = "fast"
	} else if difficulty.Score >= 1.5 || resp.Nouls()["multistep"].Noul >= 0.70 {
		if specialistCapacity && budgetAllowsSpecialist {
			strategy = "specialist"
		} else {
			strategy = "standard-review"
		}
	}
	fmt.Printf("route: %s (family=%s, difficulty=%.2f)\n", strategy, family.Choice, difficulty.Score)
}
