// Command webhook-route-review compares a verified legacy webhook's semantic
// route with a human label. Signature and deduplication remain Go checks.
package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
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

	secret := []byte("fixture-secret")
	body := []byte(`{"event_id":"evt-91","description":"The customer removed the saved shipping address."}`)
	sign := func(payload []byte) []byte {
		mac := hmac.New(sha256.New, secret)
		mac.Write(payload)
		return mac.Sum(nil)
	}
	// This independently recorded signature belongs to the fixture body.
	signature, err := hex.DecodeString("5308c6fe33715ef6525298a6050e5955d419925367f71ee6b0eb0edc631338a6")
	if err != nil {
		log.Fatal(err)
	}
	if !hmac.Equal(signature, sign(body)) || hmac.Equal(signature, sign(append(append([]byte(nil), body...), '!'))) {
		log.Fatal("deterministic signature assertion failed")
	}
	var event struct{ EventID, Description string }
	var decoded map[string]string
	if err := json.Unmarshal(body, &decoded); err != nil {
		log.Fatal(err)
	}
	event.EventID, event.Description = decoded["event_id"], decoded["description"]
	seen := map[string]bool{}
	accept := func(id string) bool {
		if seen[id] {
			return false
		}
		seen[id] = true
		return true
	}
	if event.EventID == "" || !accept(event.EventID) || accept(event.EventID) {
		log.Fatal("deterministic deduplication assertion failed")
	}
	const humanLabel = "removed"
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: event.Description,
		Questions: typesafe.Questions{"route": typesafe.Choice{
			Instructions: "Does the description say a resource was created, modified, removed, or something else?",
			Criteria:     typesafe.ChoiceCriteria{"created": "New resource added", "modified": "Existing resource changed", "removed": "Existing resource deleted or removed", "other": "No clear supported change"},
		}},
	})
	if err != nil {
		fmt.Println("INCONCLUSIVE route; verified event retained;", err)
		return
	}
	a := resp.Choices()["route"]
	if a.Confidence < 0.85 || a.Choice == "other" {
		fmt.Printf("INCONCLUSIVE %s route (%s, %.2f)\n", event.EventID, a.Choice, a.Confidence)
		return
	}
	// This bounded label is the Go adapter's proposed route. The integration
	// test asserts resulting state separately and does not waive failures.
	if a.Choice != humanLabel {
		fmt.Printf("REVIEW %s: adapter route %s disagrees with fixture %s\n", event.EventID, a.Choice, humanLabel)
	} else {
		fmt.Printf("REVIEW %s: route agrees with fixture; resulting state still needs exact Go assertions\n", event.EventID)
	}
}
