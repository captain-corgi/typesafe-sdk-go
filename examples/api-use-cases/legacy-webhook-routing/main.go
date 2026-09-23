// Command legacy-webhook-routing verifies a sample webhook before routing an
// underspecified description to a bounded internal handler.
// Requires TYPESAFE_API_KEY: go run ./examples/api-use-cases/legacy-webhook-routing
package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/captain-corgi/typesafe-sdk-go"
)

type event struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Description string `json:"description"`
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

	// A real adapter receives the secret from secure configuration and checks
	// the provider's timestamped signature format and replay window as well.
	const demoSecret = "local-demo-secret"
	body := []byte(`{"id":"evt-501","type":"change","description":"The operator removed the old payment method from the account."}`)
	mac := hmac.New(sha256.New, []byte(demoSecret))
	mac.Write(body)
	signature := hex.EncodeToString(mac.Sum(nil))
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	decision, err := routeWebhook(ctx, client, body, signature, []byte(demoSecret), map[string]bool{})
	if err != nil {
		fmt.Fprintln(os.Stderr, "webhook:", err)
	}
	fmt.Println("route:", decision)
}

func routeWebhook(ctx context.Context, client *typesafe.Client, body []byte, signature string, secret []byte, seen map[string]bool) (string, error) {
	if len(body) == 0 || len(body) > 4096 {
		return "reject_payload", nil
	}
	provided, err := hex.DecodeString(signature)
	if err != nil {
		return "reject_signature", nil
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	if !hmac.Equal(provided, mac.Sum(nil)) {
		return "reject_signature", nil
	}
	var e event
	if err := json.Unmarshal(body, &e); err != nil || e.ID == "" || len(e.Description) > 1000 {
		return "reject_payload", nil
	}
	if seen[e.ID] {
		return "duplicate", nil
	}
	seen[e.ID] = true // use a durable dedupe store in a real adapter

	// Reliable provider event types take priority over prose.
	switch e.Type {
	case "created", "modified", "removed":
		return e.Type, nil
	case "change":
		// The provider's generic type is the only case needing Jev.
	default:
		return "reconcile", nil
	}
	resp, err := client.SystemOne(ctx, &typesafe.SystemOneParams{
		State: e.Description,
		Questions: typesafe.Questions{
			"change": typesafe.Choice{
				Instructions: "Which supported change does this description report?",
				Criteria: typesafe.ChoiceCriteria{
					"created": "A new object was added", "modified": "An existing object was changed",
					"removed": "An object was deleted or detached", "other": "No supported change is clear",
				},
			},
			"reversal": typesafe.Noul{Instructions: "Does the description explicitly say this event reverses an earlier change?"},
		},
	})
	if err != nil {
		return "reconcile", err
	}
	change := resp.Choices()["change"]
	if change.Confidence < 0.80 || change.Choice == "other" {
		return "reconcile", nil
	}
	if change.Choice != "created" && change.Choice != "modified" && change.Choice != "removed" {
		return "reconcile", nil
	}
	if resp.Nouls()["reversal"].Noul > 0.20 {
		return "reconcile_reversal", nil
	}
	return change.Choice, nil
}
