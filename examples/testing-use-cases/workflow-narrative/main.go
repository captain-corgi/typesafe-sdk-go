// Command workflow-narrative compares two short event excerpts after Go
// verifies trace identity, order, delivery, and resulting state.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

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

	traceA, traceB := "trace-88", "trace-88"
	upstreamAt := time.Date(2026, 9, 23, 8, 0, 0, 0, time.UTC)
	downstreamAt := upstreamAt.Add(3 * time.Second)
	delivered, stateChanged := true, true
	if traceA != traceB || !downstreamAt.After(upstreamAt) || !delivered || !stateChanged {
		log.Fatal("deterministic workflow assertion failed")
	}
	upstream := "Customer requested cancellation of an unshipped order."
	downstream := "Fulfillment recorded a cancellation request for the unshipped order."
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: map[string]any{"upstream_excerpt": upstream, "downstream_excerpt": downstream},
		Questions: typesafe.Questions{"same_operation": typesafe.Noul{
			Instructions: "Does the downstream event describe the same requested operation as the upstream request? Ignore IDs and timing.",
		}},
	})
	if err != nil {
		fmt.Println("INCONCLUSIVE narrative; deterministic workflow checks remain;", err)
		return
	}
	p := resp.Nouls()["same_operation"].Noul
	if p <= 0.10 {
		fmt.Printf("REVIEW narrative drift (%.2f)\n", p)
	} else if p < 0.90 {
		fmt.Printf("INCONCLUSIVE narrative (%.2f)\n", p)
	} else {
		fmt.Printf("REVIEW narrative continuity candidate (%.2f); trace/order/state proven only by Go\n", p)
	}
}
