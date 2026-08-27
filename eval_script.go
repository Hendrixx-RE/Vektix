package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Hendrixx-RE/Vektix/internal/ollama"
	"github.com/Hendrixx-RE/Vektix/internal/router"
)

type TestCase struct {
	Input    string         `json:"input"`
	Expected *router.Intent `json:"expected"`
	Tier     int            `json:"tier"`
}

func main() {
	f, err := os.Open("testdata/intent_eval.jsonl")
	if err != nil {
		panic(err)
	}
	defer f.Close()

	var cases []TestCase
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var tc TestCase
		if err := json.Unmarshal(scanner.Bytes(), &tc); err != nil {
			panic(err)
		}
		cases = append(cases, tc)
	}

	client := ollama.NewClient(ollama.Options{
		Host:              "http://localhost:11434",
		IntentTimeout:     15 * time.Second,
		EmbedTimeout:      180 * time.Second,
		StreamIdleTimeout: 30 * time.Second,
	})

	ctx := context.Background()

	correctActions := 0
	correctParams := 0
	total := len(cases)

	fmt.Printf("Evaluating %d cases...\n", total)

	for i, tc := range cases {
		// Tier 1 fastpath
		intent := router.ParseFastPath(tc.Input)
		actualTier := 1

		// Fallback to Tier 2
		if intent == nil {
			actualTier = 2
			// Wait, calling actual LLM could be slow (150 * 300ms = 45s). It's fine for an eval script.
			intent, err = router.ParseLLM(ctx, client, "qwen2.5:0.5b", tc.Input)
			if err != nil {
				fmt.Printf("[%d] Error on Tier 2: %v\n", i, err)
				continue
			}
		}

		if actualTier != tc.Tier {
			fmt.Printf("[%d] %q: expected Tier %d, got Tier %d\n", i, tc.Input, tc.Tier, actualTier)
		}

		actionOk := intent.Action == tc.Expected.Action
		if actionOk {
			correctActions++
		} else {
			fmt.Printf("[%d] %q: expected action %q, got %q\n", i, tc.Input, tc.Expected.Action, intent.Action)
		}

		// check parameters
		paramsOk := true
		if tc.Expected.Query != "" && intent.Query == "" && intent.Path == "" {
			// In ambiguous intent queries, path or query might be populated. Let's just do a relaxed check.
		}
		
		if tc.Expected.Query != "" && !strings.Contains(strings.ToLower(intent.Query), strings.ToLower(tc.Expected.Query)) && !strings.Contains(strings.ToLower(intent.Path), strings.ToLower(tc.Expected.Query)) {
			// if it's completely missing
			paramsOk = false
		}
		
		if tc.Expected.Path != "" && intent.Path != tc.Expected.Path {
			// for exact path match in tier 1
			if tc.Tier == 1 {
				paramsOk = false
			}
		}

		if paramsOk {
			correctParams++
		} else {
			fmt.Printf("[%d] %q: expected params %+v, got %+v\n", i, tc.Input, tc.Expected, intent)
		}
	}

	actionAcc := float64(correctActions) / float64(total) * 100
	paramAcc := float64(correctParams) / float64(total) * 100

	fmt.Printf("\nResults:\n")
	fmt.Printf("Action Accuracy: %.2f%%\n", actionAcc)
	fmt.Printf("Parameter Extraction: %.2f%%\n", paramAcc)
}
