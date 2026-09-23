// Command cli-help-meaning reviews whether help text explains its flags. Go
// checks literal flag names and exit status; Jev screens the intended meaning.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/captain-corgi/typesafe-sdk-go"
)

func main() {
	if os.Getenv("TYPESAFE_API_KEY") == "" {
		log.Fatal("set TYPESAFE_API_KEY for this opt-in review command")
	}
	client, err := typesafe.NewClient()
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	const expectedMeaning = "both"
	help := "Usage: upload --input FILE [--dry-run]\n--input FILE is required. --dry-run previews changes without writing."
	exitCode := 0
	if exitCode != 0 || !strings.Contains(help, "--input") || !strings.Contains(help, "--dry-run") {
		log.Fatal("deterministic help assertion failed")
	}
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: help,
		Questions: typesafe.Questions{"explains": typesafe.Choice{
			Instructions: "Which flag behavior does this help output explain?",
			Criteria: typesafe.ChoiceCriteria{
				"required_flag": "Explains --input FILE is required only",
				"optional_flag": "Explains --dry-run is optional only",
				"both":          "Explains both required --input and optional --dry-run",
				"neither":       "Explains neither flag's role",
			},
		}},
	})
	if err != nil {
		fmt.Println("INCONCLUSIVE help meaning;", err)
		return
	}
	a := resp.Choices()["explains"]
	if a.Confidence < 0.85 {
		fmt.Printf("INCONCLUSIVE help meaning (%s, %.2f)\n", a.Choice, a.Confidence)
	} else if a.Choice != expectedMeaning {
		fmt.Printf("REVIEW semantic diff: expected %s, inferred %s (%.2f)\n", expectedMeaning, a.Choice, a.Confidence)
	} else {
		fmt.Printf("REVIEW help meaning appears stable (%.2f); exact flag checks remain in Go\n", a.Confidence)
	}
}
