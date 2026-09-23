// Command rest-message-result screens REST prose for a contradiction after
// Go checks status and JSON structure. Run separately from go test.
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

	statusCode := 409
	fixture := []byte(`{"result":"failed","message":"Your reservation was confirmed.","request_id":"req-72"}`)
	var body struct {
		Result    string `json:"result"`
		Message   string `json:"message"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(fixture, &body); err != nil || statusCode != 409 || body.Result != "failed" || body.RequestID == "" {
		log.Fatal("deterministic REST assertion failed")
	}
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: map[string]any{"status": statusCode, "result": body.Result, "message": body.Message},
		Questions: typesafe.Questions{"contradicts": typesafe.Noul{
			Instructions: "Does the message claim success while the status and result say the operation failed?",
			Criteria:     &typesafe.NoulCriteria{True: "Message claims the reservation succeeded", False: "Message accurately describes failure"},
		}},
	})
	if err != nil {
		fmt.Println("INCONCLUSIVE prose; structural assertions remain;", err)
		return
	}
	p := resp.Nouls()["contradicts"].Noul
	if p >= 0.90 {
		fmt.Printf("REVIEW response contradiction (%.2f)\n", p)
	} else if p > 0.10 {
		fmt.Printf("INCONCLUSIVE response prose (%.2f)\n", p)
	} else {
		fmt.Printf("REVIEW no contradiction detected (%.2f); Go assertions remain authoritative\n", p)
	}
}
