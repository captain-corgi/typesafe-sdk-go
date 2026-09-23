// Command check-documentation-citations verifies an exact quote before semantic support.
// Requires TYPESAFE_API_KEY; run with: go run ./examples/go-software-use-cases/19-check-documentation-citations
package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"strings"

	"github.com/captain-corgi/typesafe-sdk-go"
)

func main() {
	client, err := typesafe.NewClient()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer client.Close()
	claim := "WithTimeout controls the timeout of each HTTP attempt."
	if len(os.Args) > 1 {
		claim = os.Args[1]
	}
	const sourceFile = "client.go"
	const quote = "sets the per-attempt timeout"
	sourceBytes, err := os.ReadFile(sourceFile)
	if err != nil {
		fmt.Println("hold citation for review: cannot read source:", err)
		return
	}
	sourceVersion := fmt.Sprintf("sha256:%x", sha256.Sum256(sourceBytes))
	source := string(sourceBytes)
	start := strings.Index(source, quote)
	if start < 0 {
		fmt.Printf("reject citation: quoted span missing from %s at %s\n", sourceFile, sourceVersion)
		return
	}
	line := strings.Count(source[:start], "\n") + 1
	sourcePath := fmt.Sprintf("%s:%d", sourceFile, line)
	lines := strings.Split(source, "\n")
	passage := lines[line-1]
	if line < len(lines) {
		passage += "\n" + lines[line]
	}
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: map[string]any{"claim": claim, "source_passage": passage, "exact_quote": quote},
		Questions: typesafe.Questions{"relationship": typesafe.Choice{
			Instructions: "How does the surrounding source passage relate to the claim?",
			Criteria: typesafe.ChoiceCriteria{
				"supports": "The passage supports the claim", "contradicts": "The passage conflicts with the claim",
				"says_nothing": "The passage does not establish the claim",
			},
		}},
	})
	if err != nil {
		fmt.Printf("hold citation %s at %s for review: %v\n", sourcePath, sourceVersion, err)
		return
	}
	answer := resp.Choices()["relationship"]
	if answer.Confidence < 0.75 {
		fmt.Printf("hold citation %s at %s for review: uncertain\n", sourcePath, sourceVersion)
		return
	}
	if answer.Choice == "supports" {
		fmt.Printf("publish supported claim with citation %s at %s\n", sourcePath, sourceVersion)
	} else {
		fmt.Printf("flag citation %s at %s: %s\n", sourcePath, sourceVersion, answer.Choice)
	}
}
