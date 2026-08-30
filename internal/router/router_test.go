package router

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/Hendrixx-RE/Vektix/internal/ollama"
)

func TestParseFastPath(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected *Intent
	}{
		// --- True positives (Tier 1 fast-path matches) ---
		// 1. open
		{"open file", "open main.go", &Intent{Action: "open", Path: "main.go"}},
		{"open dir", "open /tmp", &Intent{Action: "open", Path: "/tmp"}},
		{"open relative path", "open src/config.json", &Intent{Action: "open", Path: "src/config.json"}},
		{"open markdown", "open README.md", &Intent{Action: "open", Path: "README.md"}},
		{"open pdf", "open docs/architecture.pdf", &Intent{Action: "open", Path: "docs/architecture.pdf"}},
		{"open nested go file", "open internal/router/fastpath.go", &Intent{Action: "open", Path: "internal/router/fastpath.go"}},
		{"open makefile", "open Makefile", &Intent{Action: "open", Path: "Makefile"}},
		{"open yaml path", "open ./deploy/docker-compose.yml", &Intent{Action: "open", Path: "./deploy/docker-compose.yml"}},
		{"open shell script", "open scripts/build.sh", &Intent{Action: "open", Path: "scripts/build.sh"}},

		// 2. read / show / cat
		{"read file", "read config.yaml", &Intent{Action: "read", Path: "config.yaml"}},
		{"read go file", "read server.go", &Intent{Action: "read", Path: "server.go"}},
		{"read nested store", "read internal/store/store.go", &Intent{Action: "read", Path: "internal/store/store.go"}},
		{"read compose yml", "read deploy/docker-compose.yml", &Intent{Action: "read", Path: "deploy/docker-compose.yml"}},
		{"read rust file", "read pkg/client.rs", &Intent{Action: "read", Path: "pkg/client.rs"}},
		{"read text file", "read docs/manual.txt", &Intent{Action: "read", Path: "docs/manual.txt"}},
		{"show file", "show config.yaml", &Intent{Action: "read", Path: "config.yaml"}},
		{"show log file", "show logs/app.log", &Intent{Action: "read", Path: "logs/app.log"}},
		{"show nested session", "show internal/session/refs.go", &Intent{Action: "read", Path: "internal/session/refs.go"}},
		{"show sql file", "show schema.sql", &Intent{Action: "read", Path: "schema.sql"}},
		{"show jsonl file", "show testdata/intent_eval.jsonl", &Intent{Action: "read", Path: "testdata/intent_eval.jsonl"}},
		{"cat file", "cat config.yaml", &Intent{Action: "read", Path: "config.yaml"}},
		{"cat nested util", "cat internal/util.go", &Intent{Action: "read", Path: "internal/util.go"}},
		{"cat syslog", "cat /var/log/syslog", &Intent{Action: "read", Path: "/var/log/syslog"}},
		{"cat shell script", "cat scripts/build.sh", &Intent{Action: "read", Path: "scripts/build.sh"}},
		{"cat toml", "cat Cargo.toml", &Intent{Action: "read", Path: "Cargo.toml"}},
		{"cat resolv conf", "cat /etc/resolv.conf", &Intent{Action: "read", Path: "/etc/resolv.conf"}},

		// 3. ls / list
		{"ls dir", "ls /usr", &Intent{Action: "list", Path: "/usr"}},
		{"ls var log", "ls /var/log", &Intent{Action: "list", Path: "/var/log"}},
		{"ls testdata dir", "ls testdata/", &Intent{Action: "list", Path: "testdata/"}},
		{"ls nested router", "ls internal/router", &Intent{Action: "list", Path: "internal/router"}},
		{"ls relative scripts", "ls ./scripts", &Intent{Action: "list", Path: "./scripts"}},
		{"ls bin dir", "ls /usr/local/bin", &Intent{Action: "list", Path: "/usr/local/bin"}},
		{"list home config", "list ~/.config", &Intent{Action: "list", Path: "~/.config"}},
		{"list components", "list src/components", &Intent{Action: "list", Path: "src/components"}},
		{"list nginx conf", "list /etc/nginx/conf.d", &Intent{Action: "list", Path: "/etc/nginx/conf.d"}},
		{"list internal store", "list internal/store", &Intent{Action: "list", Path: "internal/store"}},
		{"list pkg models", "list pkg/models", &Intent{Action: "list", Path: "pkg/models"}},

		// 4. head
		{"head with num", "head 30 main.go", &Intent{Action: "read", Lines: "30", Path: "main.go"}},
		{"head readme", "head 30 README.md", &Intent{Action: "read", Lines: "30", Path: "README.md"}},
		{"head refs go", "head 50 internal/session/refs.go", &Intent{Action: "read", Lines: "50", Path: "internal/session/refs.go"}},
		{"head negative num", "head -20 log.txt", &Intent{Action: "read", Lines: "-20", Path: "log.txt"}},
		{"head main go nested", "head 15 cmd/vektix/main.go", &Intent{Action: "read", Lines: "15", Path: "cmd/vektix/main.go"}},
		{"head schema json", "head 100 data/schema.json", &Intent{Action: "read", Lines: "100", Path: "data/schema.json"}},
		{"head syslog", "head -5 /var/log/syslog", &Intent{Action: "read", Lines: "-5", Path: "/var/log/syslog"}},

		// 5. tail
		{"tail with -num", "tail -100 main.go", &Intent{Action: "read", Lines: "-100", Path: "main.go"}},
		{"tail temp txt", "tail -50 temp.txt", &Intent{Action: "read", Lines: "-50", Path: "temp.txt"}},
		{"tail nginx log", "tail 25 /var/log/nginx.log", &Intent{Action: "read", Lines: "25", Path: "/var/log/nginx.log"}},
		{"tail output log", "tail 40 output.log", &Intent{Action: "read", Lines: "40", Path: "output.log"}},
		{"tail env file", "tail -10 .env", &Intent{Action: "read", Lines: "-10", Path: ".env"}},
		{"tail config go", "tail 15 internal/config/config.go", &Intent{Action: "read", Lines: "15", Path: "internal/config/config.go"}},

		// 6. find
		{"find glob", "find *.go", &Intent{Action: "locate", Query: "*.go"}},
		{"find ts glob", "find src/*.ts", &Intent{Action: "locate", Query: "src/*.ts"}},
		{"find question mark glob", "find ?est.go", &Intent{Action: "locate", Query: "?est.go"}},
		{"find bracket glob", "find [a-z]*.py", &Intent{Action: "locate", Query: "[a-z]*.py"}},
		{"find recursive glob", "find internal/**/*.go", &Intent{Action: "locate", Query: "internal/**/*.go"}},
		{"find toml single token", "find config.toml", &Intent{Action: "locate", Query: "config.toml"}},
		{"find makefile", "find Makefile", &Intent{Action: "locate", Query: "Makefile"}},
		{"find dockerfile", "find Dockerfile", &Intent{Action: "locate", Query: "Dockerfile"}},
		{"find log glob", "find /var/log/*.log", &Intent{Action: "locate", Query: "/var/log/*.log"}},

		// 7. copy (paths and session references)
		{"copy config path", "copy config.yaml", &Intent{Action: "copy", Path: "config.yaml"}},
		{"copy ts path", "copy src/utils.ts", &Intent{Action: "copy", Path: "src/utils.ts"}},
		{"copy fastpath go", "copy internal/router/fastpath.go", &Intent{Action: "copy", Path: "internal/router/fastpath.go"}},
		{"copy etc hosts", "copy /etc/hosts", &Intent{Action: "copy", Path: "/etc/hosts"}},
		{"copy readme", "copy README.md", &Intent{Action: "copy", Path: "README.md"}},
		{"copy ref it", "copy it", &Intent{Action: "copy", Path: "it"}},
		{"copy ref that", "copy that", &Intent{Action: "copy", Path: "that"}},
		{"copy ref this", "copy this", &Intent{Action: "copy", Path: "this"}},
		{"copy ordinal ref #2", "copy #2", &Intent{Action: "copy", Path: "#2"}},
		{"copy ordinal ref #15", "copy #15", &Intent{Action: "copy", Path: "#15"}},
		{"copy ordinal 2nd", "copy 2nd", &Intent{Action: "copy", Path: "2nd"}},
		{"copy ordinal 3rd", "copy 3rd", &Intent{Action: "copy", Path: "3rd"}},
		{"copy the first one", "copy the first one", &Intent{Action: "copy", Path: "the first one"}},
		{"copy the second one", "copy the second one", &Intent{Action: "copy", Path: "the second one"}},
		{"copy the third one", "copy the third one", &Intent{Action: "copy", Path: "the third one"}},
		{"copy the fourth one", "copy the fourth one", &Intent{Action: "copy", Path: "the fourth one"}},
		{"copy the 4th one", "copy the 4th one", &Intent{Action: "copy", Path: "the 4th one"}},
		{"copy the fifth one", "copy the fifth one", &Intent{Action: "copy", Path: "the fifth one"}},
		{"copy the last one", "copy the last one", &Intent{Action: "copy", Path: "the last one"}},
		{"copy demonstrative that pdf", "copy that pdf", &Intent{Action: "copy", Path: "that pdf"}},
		{"copy demonstrative the go file", "copy the go file", &Intent{Action: "copy", Path: "the go file"}},
		{"copy demonstrative this code", "copy this code", &Intent{Action: "copy", Path: "this code"}},
		{"copy demonstrative that document", "copy that document", &Intent{Action: "copy", Path: "that document"}},
		{"copy pronoun the excerpt", "copy the excerpt", &Intent{Action: "copy", Path: "the excerpt"}},
		{"copy pronoun the result", "copy the result", &Intent{Action: "copy", Path: "the result"}},
		{"copy pronoun the file", "copy the file", &Intent{Action: "copy", Path: "the file"}},
		{"copy pronoun the match", "copy the match", &Intent{Action: "copy", Path: "the match"}},

		// --- Fail guards / Hijack attempts (must fall through to Tier 2, returning nil) ---
		// 1. open hijacks
		{"open hijack hal", "open the pod bay doors hal", nil},
		{"open hijack new issue", "open a new issue about the database crash", nil},
		{"open hijack discussion", "open up the discussion on pull requests", nil},
		{"open hijack sesame", "open sesame and show all secrets", nil},
		{"open hijack questions", "open questions from yesterday meeting", nil},
		{"open hijack mind", "open your mind to modern backend design", nil},
		{"open hijack ticket", "open up ticket 123 for discussion", nil},

		// 2. read hijacks
		{"read hijack lines", "read between the lines on this PR", nil},
		{"read hijack deployment error", "read me the latest deployment error", nil},
		{"read hijack memo", "read the memo about postgres migration", nil},
		{"read hijack comments", "read through the comments on ticket 404", nil},
		{"read hijack aloud", "read aloud the summary of the meeting", nil},
		{"read hijack release plans", "read all about our release plans", nil},

		// 3. show hijacks
		{"show hijack docker file", "show me what's in the docker file", nil},
		{"show hijack reproduce bug", "show us how to reproduce the bug", nil},
		{"show hijack benchmark results", "show the team the latest benchmark results", nil},
		{"show hijack error handling", "show some examples of error handling", nil},
		{"show hijack outage details", "show all details regarding the outage", nil},
		{"show hijack auth flow", "show how the authentication flow works", nil},

		// 4. cat hijacks
		{"cat hijack cloud", "cat photos are stored in the cloud", nil},
		{"cat hijack standup", "cat got your tongue during the standup", nil},
		{"cat hijack receipt", "cat food receipt from last week", nil},
		{"cat hijack favorite animal", "cat is not my favorite animal", nil},
		{"cat hijack guidelines", "cat and dog adoption guidelines", nil},
		{"cat hijack distracting bug", "cat videos are distracting me from the bug", nil},

		// 5. ls hijacks
		{"ls hijack alias", "ls alias for kubernetes cluster", nil},
		{"ls hijack bash options", "ls command options in bash", nil},
		{"ls hijack sort flags", "ls flags for sorting by time", nil},
		{"ls hijack colors", "ls output formatting with colors", nil},
		{"ls hijack tricks", "ls tricks for searching directories", nil},

		// 6. list hijacks
		{"list hijack rust", "list out the top reasons for adopting rust", nil},
		{"list hijack dependencies", "list of dependencies for this project", nil},
		{"list hijack db connections", "list all active database connections", nil},
		{"list hijack contributors", "list every contributor to the repository", nil},
		{"list hijack fix before release", "list things we need to fix before release", nil},
		{"list hijack query failed", "list reasons why the query failed", nil},

		// 7. head hijacks
		{"head hijack with number reasons", "head 10 reasons why we use go", nil},
		{"head hijack with negative number steps", "head -5 steps to reproduce the issue", nil},
		{"head hijack with number tips", "head 20 tips for writing clean code", nil},
		{"head hijack with number roadmap", "head 5 items on the roadmap", nil},
		{"head hijack with number optimize", "head 100 ways to optimize database queries", nil},
		{"head hijack straight db", "head straight to the database section", nil},
		{"head hijack marketing", "head of the marketing department", nil},
		{"head hijack settings", "head over to the settings page", nil},
		{"head hijack first into refactor", "head first into the codebase refactor", nil},

		// 8. tail hijacks
		{"tail hijack with number todo", "tail 5 items on the todo list", nil},
		{"tail hijack with negative number survey", "tail -10 questions from the survey", nil},
		{"tail hijack with number learned", "tail 100 things I learned this year", nil},
		{"tail hijack with number pr comments", "tail 3 comments on the pull request", nil},
		{"tail hijack with number legacy stack", "tail 50 problems with our legacy stack", nil},
		{"tail hijack meeting notes", "tail end of the meeting notes from yesterday", nil},
		{"tail hijack compiler", "tail call optimization in the compiler", nil},
		{"tail hijack latency", "tail latency issues with the redis cache", nil},
		{"tail hijack chasing", "tail chasing in the planning meeting", nil},

		// 9. find hijacks
		{"find hijack docker", "find out what I wrote about docker", nil},
		{"find hijack auth middleware", "find the auth middleware file", nil},
		{"find hijack timeouts", "find where we handle network timeouts", nil},
		{"find hijack retirement", "find information about the retirement plan", nil},
		{"find hijack kubernetes expert", "find someone who knows about kubernetes", nil},
		{"find hijack ci failing", "find why the build is failing on ci", nil},
		{"find hijack legacy API", "find all references to the legacy API", nil},
		{"find hijack memory leaks", "find the bug causing memory leaks", nil},

		// 10. copy hijacks
		{"copy hijack unstructured", "copy this text here", nil},
		{"copy hijack clipboard connection", "copy the connection string to clipboard", nil},
		{"copy hijack paste error", "copy and paste the error message", nil},
		{"copy hijack homework", "copy my homework from yesterday", nil},
		{"copy hijack discussed earlier", "copy everything we discussed earlier", nil},
		{"copy hijack email draft", "copy that into the email draft", nil},
		{"copy hijack backup location", "copy all files to backup location", nil},
		{"copy hijack staging password", "copy the database password for staging", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseFastPath(tt.input)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("ParseFastPath(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestParseLLM(t *testing.T) {
	importJSON := `{"message": {"content": "{\"action\":\"excerpt\",\"query\":\"docker\"}"}}`

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(importJSON))
	}))
	defer ts.Close()

	client := ollama.NewClient(ollama.Options{
		Host:              ts.URL,
		IntentTimeout:     15 * time.Second,
		EmbedTimeout:      180 * time.Second,
		StreamIdleTimeout: 30 * time.Second,
	})

	intent, err := ParseLLM(context.Background(), client, "qwen2.5:0.5b", "find out what I wrote about docker")
	if err != nil {
		t.Fatalf("ParseLLM failed: %v", err)
	}

	expected := &Intent{Action: "excerpt", Query: "docker"}
	if !reflect.DeepEqual(intent, expected) {
		t.Errorf("ParseLLM() = %v, want %v", intent, expected)
	}
}

func TestIntentEvalJSONLConsistency(t *testing.T) {
	f, err := os.Open("../../testdata/intent_eval.jsonl")
	if err != nil {
		t.Fatalf("failed to open testdata/intent_eval.jsonl: %v", err)
	}
	defer f.Close()

	type evalCase struct {
		Input    string  `json:"input"`
		Expected *Intent `json:"expected"`
		Tier     int     `json:"tier"`
	}

	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		var tc evalCase
		if err := json.Unmarshal(scanner.Bytes(), &tc); err != nil {
			t.Fatalf("line %d: json unmarshal failed: %v", lineNum, err)
		}

		got := ParseFastPath(tc.Input)
		if tc.Tier == 1 {
			if got == nil {
				t.Errorf("line %d (%q): expected Tier 1 match, got nil", lineNum, tc.Input)
				continue
			}
			if !reflect.DeepEqual(got, tc.Expected) {
				t.Errorf("line %d (%q): ParseFastPath() = %+v, want %+v", lineNum, tc.Input, got, tc.Expected)
			}
		} else if tc.Tier == 2 {
			if got != nil {
				t.Errorf("line %d (%q): expected Tier 2 fallthrough (got=nil), but fastpath hijacked to: %+v", lineNum, tc.Input, got)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner error: %v", err)
	}
}

