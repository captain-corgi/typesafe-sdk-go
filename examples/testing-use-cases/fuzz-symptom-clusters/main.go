// Command fuzz-symptom-clusters suggests a symptom cluster after exact
// stack/input hash grouping. It never deletes a reproducing corpus input.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/captain-corgi/typesafe-sdk-go"
)

type failure struct{ id, inputHash, stackHash, summary string }

func main() {
	if os.Getenv("TYPESAFE_API_KEY") == "" {
		log.Fatal("set TYPESAFE_API_KEY for this opt-in triage command")
	}
	client, err := typesafe.NewClient()
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	a := failure{"corpus-A", "sha256:a1", "stack:44", "Parser panics when a nested closing brace follows an empty field."}
	b := failure{"corpus-B", "sha256:b2", "stack:73", "Decoder crashes while closing a nested field containing no text."}
	if a.inputHash == b.inputHash {
		fmt.Println("same input hash: retain both corpus paths until deterministic deduplication review")
		return
	}
	if a.stackHash == b.stackHash {
		fmt.Println("same stack hash: exact cluster candidate; retain both corpus inputs")
		return
	}
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: map[string]any{"summary_a": a.summary, "summary_b": b.summary},
		Questions: typesafe.Questions{"symptom": typesafe.Choice{
			Instructions: "Do these short failure reports describe the same symptom, different symptoms, or is the context unclear?",
			Criteria:     typesafe.ChoiceCriteria{"same_symptom": "Same observable failure", "different_symptoms": "Distinct observable failures", "unclear": "Not enough context"},
		}},
	})
	if err != nil {
		fmt.Println("INCONCLUSIVE: retain both corpus inputs;", err)
		return
	}
	answer := resp.Choices()["symptom"]
	if answer.Confidence < 0.85 || answer.Choice == "unclear" {
		fmt.Printf("INCONCLUSIVE: retain %s and %s (%s, %.2f)\n", a.id, b.id, answer.Choice, answer.Confidence)
	} else {
		fmt.Printf("REVIEW %s and %s: %s (%.2f); retain both until deterministic tests prove redundancy\n", a.id, b.id, answer.Choice, answer.Confidence)
	}
}
