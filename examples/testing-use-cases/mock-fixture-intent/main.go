// Command mock-fixture-intent checks whether mock response prose fits the
// intended failure after deterministic status and JSON checks in Go.
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

	scenario := "The upstream inventory service times out."
	status := 503
	body := []byte(`{"code":"UPSTREAM_TIMEOUT","message":"Inventory service did not respond before the deadline."}`)
	var decoded struct{ Code, Message string }
	if err := json.Unmarshal(body, &decoded); err != nil || status != 503 || decoded.Code != "UPSTREAM_TIMEOUT" || decoded.Message == "" {
		log.Fatal("deterministic fixture assertion failed")
	}
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State:     map[string]any{"scenario": scenario, "mock_response_text": decoded.Message},
		Questions: typesafe.Questions{"fits": typesafe.Noul{Instructions: "Does the mock response text depict the intended failure rather than a different failure?"}},
	})
	if err != nil {
		fmt.Println("INCONCLUSIVE fixture prose; deterministic checks passed;", err)
		return
	}
	p := resp.Nouls()["fits"].Noul
	if p <= 0.10 {
		fmt.Printf("REVIEW fixture drift (%.2f)\n", p)
	} else if p < 0.90 {
		fmt.Printf("INCONCLUSIVE fixture intent (%.2f)\n", p)
	} else {
		fmt.Printf("REVIEW fixture prose appears aligned (%.2f); status and JSON checks remain authoritative\n", p)
	}
}
