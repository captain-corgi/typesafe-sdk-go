// Command fuzz-failure-triage labels a saved reproducing fuzz failure for
// routing. It never claims that Jev discovers or confirms the defect.
package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"os"

	"github.com/captain-corgi/typesafe-sdk-go"
)

func main() {
	if os.Getenv("TYPESAFE_API_KEY") == "" {
		log.Fatal("set TYPESAFE_API_KEY for this opt-in triage command")
	}
	client, err := typesafe.NewClient()
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	corpusID := "testdata/fuzz/FuzzDecode/7b1a..."
	input := []byte{0xff, 0x00, 0xff}
	failure := "panic: index out of range in Decode at parser.go:88"
	replayExitCode := 1 // Produced by a separate deterministic Go replay.
	if replayExitCode == 0 || len(input) == 0 || corpusID == "" {
		log.Fatal("no reproducing failure to triage")
	}
	fingerprint := sha256.Sum256(input)
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: failure,
		Questions: typesafe.Questions{"failure_kind": typesafe.Choice{
			Instructions: "Which failure class does this short Go fuzz failure report describe?",
			Criteria: typesafe.ChoiceCriteria{
				"panic": "A Go panic", "round_trip_mismatch": "Round-trip property mismatch", "unexpected_error": "Unexpected returned error", "timeout": "Execution timed out", "other": "None fits",
			},
		}},
	})
	if err != nil {
		fmt.Printf("INCONCLUSIVE %s (%x): retain corpus entry; %v\n", corpusID, fingerprint[:6], err)
		return
	}
	a := resp.Choices()["failure_kind"]
	if a.Confidence < 0.85 || a.Choice == "other" {
		fmt.Printf("INCONCLUSIVE %s: retain corpus entry (%s, %.2f)\n", corpusID, a.Choice, a.Confidence)
		return
	}
	fmt.Printf("REVIEW %s: tentative owner for %s (%.2f), input SHA-256 %x; keep corpus entry\n", corpusID, a.Choice, a.Confidence, fingerprint)
}
