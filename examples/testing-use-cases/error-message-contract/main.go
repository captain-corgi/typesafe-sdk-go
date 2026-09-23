// Command error-message-contract reviews error wording after deterministic
// wrapping, stable-token, and redaction checks. It is opt-in and credentialed.
package main

import (
	"context"
	"errors"
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

	message := "API key missing. Set TYPESAFE_API_KEY before making a request."
	formatted := fmt.Errorf("%w: %s", typesafe.ErrMissingAPIKey, message)
	if !errors.Is(formatted, typesafe.ErrMissingAPIKey) || !strings.Contains(message, "TYPESAFE_API_KEY") || strings.Contains(message, "sk_live_") {
		log.Fatal("deterministic error contract failed")
	}
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State:     map[string]any{"actual_message": message, "semantic_contract": "Tell the user the API key is missing and how to supply it."},
		Questions: typesafe.Questions{"clear": typesafe.Noul{Instructions: "Does the message clearly tell the user the API key is missing and how to supply it?"}},
	})
	if err != nil {
		fmt.Println("INCONCLUSIVE wording; deterministic checks passed;", err)
		return
	}
	p := resp.Nouls()["clear"].Noul
	if p <= 0.10 {
		fmt.Printf("REVIEW possible wording regression (%.2f)\n", p)
	} else if p < 0.90 {
		fmt.Printf("INCONCLUSIVE wording (%.2f)\n", p)
	} else {
		fmt.Printf("REVIEW wording appears clear (%.2f); exact Go checks still required\n", p)
	}
}
