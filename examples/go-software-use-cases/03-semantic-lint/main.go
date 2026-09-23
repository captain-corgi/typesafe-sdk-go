// Command semantic-lint checks a contextual Go convention after deterministic syntax parsing.
// Requires TYPESAFE_API_KEY; run with: go run ./examples/go-software-use-cases/03-semantic-lint
package main

import (
	"context"
	"fmt"
	"go/parser"
	"go/token"
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
	code := `package api
func CreateUser(raw string) error {
	return databaseInsert(raw)
}`
	if _, err := parser.ParseFile(token.NewFileSet(), "change.go", code, parser.AllErrors); err != nil {
		fmt.Println("Compiler-side syntax check failed:", err)
		return
	}
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: map[string]any{
			"convention":      "API handlers validate user input before calling the database layer.",
			"changed_go_code": code,
		},
		Questions: typesafe.Questions{
			"bypasses_validation": typesafe.Noul{Instructions: "Does this changed path appear to bypass input validation before a database write?"},
			"severity": typesafe.Score{Instructions: "How severe would that convention violation be?", Criteria: typesafe.ScoreCriteria{
				"No plausible violation", "Review recommended", "Likely production risk",
			}},
		},
	})
	if err != nil {
		fmt.Println("Semantic lint unavailable; send this change to review:", err)
		return
	}
	probability := resp.Nouls()["bypasses_validation"].Noul
	severity := resp.Scores()["severity"]
	if probability >= 0.85 && severity.Confidence >= 0.65 && severity.Score >= 1.5 {
		fmt.Printf("flag for review: validation bypass p=%.2f severity=%.2f\n", probability, severity.Score)
	} else if probability <= 0.20 && severity.Confidence >= 0.65 {
		fmt.Println("No semantic finding; run gofmt, go vet, and tests as usual.")
	} else {
		fmt.Println("Uncertain semantic result; send this change to review.")
	}
}
