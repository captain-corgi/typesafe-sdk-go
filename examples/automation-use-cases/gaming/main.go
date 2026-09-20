// Command gaming demonstrates player-support automation: a report plus
// chat log assessed for credible cheating, toxic chat, and churn signals,
// producing sorted plain-Go actions from anti-cheat escalation to win-back
// outreach.
//
// It mirrors the "Gaming" entry of the docs use-case map's example
// automation use cases
// (https://docs.typesafe.ai/concepts/use-case-map#example-automation-use-cases).
//
// Requires TYPESAFE_API_KEY; run with:
//
//	go run ./examples/automation-use-cases/gaming
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"

	"github.com/captain-corgi/typesafe-sdk-go"
)

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

	cases := []map[string]any{
		{
			"player_report": "Snapped to targets through walls all round. Spectated for 3 rounds " +
				"— tracking is impossible at that range.",
			"chat_log": "gg wp — nice comeback, see you tomorrow same squad?",
			"account":  map[string]any{"age_days": 412, "spend_usd": 60, "sessions_last_30d": 22},
		},
		{
			"player_report": "Refused to play objectives, fed kills to the enemy team on purpose.",
			"chat_log": "This game is dead and so is my wallet. Uninstalling, want my money back. " +
				"Enjoy the trash matchmaking, everyone.",
			"account": map[string]any{"age_days": 88, "spend_usd": 140, "sessions_last_30d": 2},
		},
	}

	for _, c := range cases {
		fmt.Println("player report:", c["player_report"])
		handle(client, c)
		fmt.Println()
	}
}

func handle(client *typesafe.Client, c map[string]any) {
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: c,
		Questions: typesafe.Questions{
			"credible_cheating": typesafe.Noul{
				Instructions: "Is this cheating report credible?",
				Criteria: &typesafe.NoulCriteria{
					True:  "Specific impossible behavior a spectator could observe (tracking through walls, snap aim)",
					False: "Vague skill accusations or salt",
				},
			},
			"toxic_chat": typesafe.Noul{
				Instructions: "Does the chat log contain abuse or harassment worth moderating?",
			},
			"frustration": typesafe.Score{
				Instructions: "How frustrated is this player?",
				Criteria:     typesafe.ScoreCriteria{"mild", "heated", "tilting"},
			},
			"churn_risk": typesafe.Noul{
				Instructions: "Given the report, chat, and account signals, is this player at " +
					"risk of quitting the game?",
			},
		},
	})
	if err != nil {
		fmt.Println("  error:", err)
		return
	}

	cheating := resp.Nouls()["credible_cheating"].Noul
	toxic := resp.Nouls()["toxic_chat"].Noul
	frustration := resp.Scores()["frustration"]
	churn := resp.Nouls()["churn_risk"].Noul

	fmt.Printf("  credible cheating: %.2f\n", cheating)
	fmt.Printf("  toxic chat: %.2f\n", toxic)
	fmt.Printf("  frustration: %.2f/2 (confidence %.2f)\n", frustration.Score, frustration.Confidence)
	fmt.Printf("  churn risk: %.2f\n", churn)

	var actions []string
	if cheating >= 0.5 {
		actions = append(actions, "escalate to anti-cheat review with replay clip")
	}
	if toxic >= 0.5 {
		actions = append(actions, "chat mute + conduct warning")
	}
	if churn >= 0.5 {
		actions = append(actions, "win-back outreach: support contact + retention offer")
	}
	if len(actions) == 0 {
		actions = append(actions, "no action — close report")
	}
	slices.Sort(actions)
	for _, action := range actions {
		fmt.Println("  action:", action)
	}
}
