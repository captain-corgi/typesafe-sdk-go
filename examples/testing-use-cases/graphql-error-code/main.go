// Command graphql-error-code compares a bounded reading of error prose with
// the stable GraphQL extension code after deterministic JSON checks.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

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

	fixture := []byte(`{"data":{"item":null},"errors":[{"message":"You do not have permission to view this item.","path":["item"],"extensions":{"code":"UNAUTHORIZED"}}]}`)
	var body struct {
		Data   map[string]json.RawMessage `json:"data"`
		Errors []struct {
			Message    string   `json:"message"`
			Path       []string `json:"path"`
			Extensions struct {
				Code string `json:"code"`
			} `json:"extensions"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(fixture, &body); err != nil || len(body.Errors) != 1 || string(body.Data["item"]) != "null" || body.Errors[0].Extensions.Code != "UNAUTHORIZED" || len(body.Errors[0].Path) != 1 {
		log.Fatal("deterministic GraphQL assertion failed")
	}
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: body.Errors[0].Message,
		Questions: typesafe.Questions{"meaning": typesafe.Choice{
			Instructions: "Which condition does this error message describe?",
			Criteria:     typesafe.ChoiceCriteria{"not_found": "Requested resource does not exist", "unauthorized": "Caller lacks permission", "validation": "Input fails validation", "other": "None of these"},
		}},
	})
	if err != nil {
		fmt.Println("INCONCLUSIVE error prose; GraphQL checks remain;", err)
		return
	}
	a := resp.Choices()["meaning"]
	if a.Confidence < 0.85 || a.Choice == "other" {
		fmt.Printf("INCONCLUSIVE error meaning (%s, %.2f)\n", a.Choice, a.Confidence)
	} else if a.Choice != "unauthorized" {
		fmt.Printf("REVIEW code/prose mismatch: code UNAUTHORIZED, prose %s (%.2f)\n", a.Choice, a.Confidence)
	} else {
		fmt.Printf("REVIEW code/prose agreement (%.2f); structure stays a Go assertion\n", a.Confidence)
	}
}
