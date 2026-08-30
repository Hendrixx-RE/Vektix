import json

cases = []

# ==============================================================================
# Tier 1 cases (Guarded regex fast-path matches)
# ==============================================================================

# 1. open
cases.extend([
    {"input": "open main.go", "expected": {"action": "open", "path": "main.go"}, "tier": 1},
    {"input": "open /etc/hosts", "expected": {"action": "open", "path": "/etc/hosts"}, "tier": 1},
    {"input": "open src/config.json", "expected": {"action": "open", "path": "src/config.json"}, "tier": 1},
    {"input": "open README.md", "expected": {"action": "open", "path": "README.md"}, "tier": 1},
    {"input": "open docs/architecture.pdf", "expected": {"action": "open", "path": "docs/architecture.pdf"}, "tier": 1},
    {"input": "open internal/router/fastpath.go", "expected": {"action": "open", "path": "internal/router/fastpath.go"}, "tier": 1},
    {"input": "open Makefile", "expected": {"action": "open", "path": "Makefile"}, "tier": 1},
    {"input": "open ./deploy/docker-compose.yml", "expected": {"action": "open", "path": "./deploy/docker-compose.yml"}, "tier": 1},
    {"input": "open scripts/build.sh", "expected": {"action": "open", "path": "scripts/build.sh"}, "tier": 1},
])

# 2. read / show / cat
cases.extend([
    # read
    {"input": "read server.go", "expected": {"action": "read", "path": "server.go"}, "tier": 1},
    {"input": "read internal/store/store.go", "expected": {"action": "read", "path": "internal/store/store.go"}, "tier": 1},
    {"input": "read deploy/docker-compose.yml", "expected": {"action": "read", "path": "deploy/docker-compose.yml"}, "tier": 1},
    {"input": "read pkg/client.rs", "expected": {"action": "read", "path": "pkg/client.rs"}, "tier": 1},
    {"input": "read docs/manual.txt", "expected": {"action": "read", "path": "docs/manual.txt"}, "tier": 1},
    # show
    {"input": "show logs/app.log", "expected": {"action": "read", "path": "logs/app.log"}, "tier": 1},
    {"input": "show config.yaml", "expected": {"action": "read", "path": "config.yaml"}, "tier": 1},
    {"input": "show internal/session/refs.go", "expected": {"action": "read", "path": "internal/session/refs.go"}, "tier": 1},
    {"input": "show schema.sql", "expected": {"action": "read", "path": "schema.sql"}, "tier": 1},
    {"input": "show testdata/intent_eval.jsonl", "expected": {"action": "read", "path": "testdata/intent_eval.jsonl"}, "tier": 1},
    # cat
    {"input": "cat internal/util.go", "expected": {"action": "read", "path": "internal/util.go"}, "tier": 1},
    {"input": "cat /var/log/syslog", "expected": {"action": "read", "path": "/var/log/syslog"}, "tier": 1},
    {"input": "cat scripts/build.sh", "expected": {"action": "read", "path": "scripts/build.sh"}, "tier": 1},
    {"input": "cat Cargo.toml", "expected": {"action": "read", "path": "Cargo.toml"}, "tier": 1},
    {"input": "cat /etc/resolv.conf", "expected": {"action": "read", "path": "/etc/resolv.conf"}, "tier": 1},
])

# 3. ls / list
cases.extend([
    {"input": "ls /usr", "expected": {"action": "list", "path": "/usr"}, "tier": 1},
    {"input": "ls /var/log", "expected": {"action": "list", "path": "/var/log"}, "tier": 1},
    {"input": "ls testdata/", "expected": {"action": "list", "path": "testdata/"}, "tier": 1},
    {"input": "ls internal/router", "expected": {"action": "list", "path": "internal/router"}, "tier": 1},
    {"input": "ls ./scripts", "expected": {"action": "list", "path": "./scripts"}, "tier": 1},
    {"input": "ls /usr/local/bin", "expected": {"action": "list", "path": "/usr/local/bin"}, "tier": 1},
    {"input": "list ~/.config", "expected": {"action": "list", "path": "~/.config"}, "tier": 1},
    {"input": "list src/components", "expected": {"action": "list", "path": "src/components"}, "tier": 1},
    {"input": "list /etc/nginx/conf.d", "expected": {"action": "list", "path": "/etc/nginx/conf.d"}, "tier": 1},
    {"input": "list internal/store", "expected": {"action": "list", "path": "internal/store"}, "tier": 1},
    {"input": "list pkg/models", "expected": {"action": "list", "path": "pkg/models"}, "tier": 1},
])

# 4. head
cases.extend([
    {"input": "head 30 main.go", "expected": {"action": "read", "path": "main.go", "lines": "30"}, "tier": 1},
    {"input": "head 30 README.md", "expected": {"action": "read", "path": "README.md", "lines": "30"}, "tier": 1},
    {"input": "head 50 internal/session/refs.go", "expected": {"action": "read", "path": "internal/session/refs.go", "lines": "50"}, "tier": 1},
    {"input": "head -20 log.txt", "expected": {"action": "read", "path": "log.txt", "lines": "-20"}, "tier": 1},
    {"input": "head 15 cmd/vektix/main.go", "expected": {"action": "read", "path": "cmd/vektix/main.go", "lines": "15"}, "tier": 1},
    {"input": "head 100 data/schema.json", "expected": {"action": "read", "path": "data/schema.json", "lines": "100"}, "tier": 1},
    {"input": "head -5 /var/log/syslog", "expected": {"action": "read", "path": "/var/log/syslog", "lines": "-5"}, "tier": 1},
])

# 5. tail
cases.extend([
    {"input": "tail -100 main.go", "expected": {"action": "read", "path": "main.go", "lines": "-100"}, "tier": 1},
    {"input": "tail -50 temp.txt", "expected": {"action": "read", "path": "temp.txt", "lines": "-50"}, "tier": 1},
    {"input": "tail 25 /var/log/nginx.log", "expected": {"action": "read", "path": "/var/log/nginx.log", "lines": "25"}, "tier": 1},
    {"input": "tail 40 output.log", "expected": {"action": "read", "path": "output.log", "lines": "40"}, "tier": 1},
    {"input": "tail -10 .env", "expected": {"action": "read", "path": ".env", "lines": "-10"}, "tier": 1},
    {"input": "tail 15 internal/config/config.go", "expected": {"action": "read", "path": "internal/config/config.go", "lines": "15"}, "tier": 1},
])

# 6. find
cases.extend([
    {"input": "find *.go", "expected": {"action": "locate", "query": "*.go"}, "tier": 1},
    {"input": "find src/*.ts", "expected": {"action": "locate", "query": "src/*.ts"}, "tier": 1},
    {"input": "find ?est.go", "expected": {"action": "locate", "query": "?est.go"}, "tier": 1},
    {"input": "find [a-z]*.py", "expected": {"action": "locate", "query": "[a-z]*.py"}, "tier": 1},
    {"input": "find internal/**/*.go", "expected": {"action": "locate", "query": "internal/**/*.go"}, "tier": 1},
    {"input": "find config.toml", "expected": {"action": "locate", "query": "config.toml"}, "tier": 1},
    {"input": "find Makefile", "expected": {"action": "locate", "query": "Makefile"}, "tier": 1},
    {"input": "find Dockerfile", "expected": {"action": "locate", "query": "Dockerfile"}, "tier": 1},
    {"input": "find /var/log/*.log", "expected": {"action": "locate", "query": "/var/log/*.log"}, "tier": 1},
])

# 7. copy
cases.extend([
    # paths
    {"input": "copy config.yaml", "expected": {"action": "copy", "path": "config.yaml"}, "tier": 1},
    {"input": "copy src/utils.ts", "expected": {"action": "copy", "path": "src/utils.ts"}, "tier": 1},
    {"input": "copy internal/router/fastpath.go", "expected": {"action": "copy", "path": "internal/router/fastpath.go"}, "tier": 1},
    {"input": "copy /etc/hosts", "expected": {"action": "copy", "path": "/etc/hosts"}, "tier": 1},
    {"input": "copy README.md", "expected": {"action": "copy", "path": "README.md"}, "tier": 1},
    # session refs
    {"input": "copy it", "expected": {"action": "copy", "path": "it"}, "tier": 1},
    {"input": "copy that", "expected": {"action": "copy", "path": "that"}, "tier": 1},
    {"input": "copy this", "expected": {"action": "copy", "path": "this"}, "tier": 1},
    {"input": "copy #2", "expected": {"action": "copy", "path": "#2"}, "tier": 1},
    {"input": "copy #15", "expected": {"action": "copy", "path": "#15"}, "tier": 1},
    {"input": "copy 2nd", "expected": {"action": "copy", "path": "2nd"}, "tier": 1},
    {"input": "copy 3rd", "expected": {"action": "copy", "path": "3rd"}, "tier": 1},
    {"input": "copy the first one", "expected": {"action": "copy", "path": "the first one"}, "tier": 1},
    {"input": "copy the second one", "expected": {"action": "copy", "path": "the second one"}, "tier": 1},
    {"input": "copy the third one", "expected": {"action": "copy", "path": "the third one"}, "tier": 1},
    {"input": "copy the fourth one", "expected": {"action": "copy", "path": "the fourth one"}, "tier": 1},
    {"input": "copy the 4th one", "expected": {"action": "copy", "path": "the 4th one"}, "tier": 1},
    {"input": "copy the fifth one", "expected": {"action": "copy", "path": "the fifth one"}, "tier": 1},
    {"input": "copy the last one", "expected": {"action": "copy", "path": "the last one"}, "tier": 1},
    {"input": "copy that pdf", "expected": {"action": "copy", "path": "that pdf"}, "tier": 1},
    {"input": "copy the go file", "expected": {"action": "copy", "path": "the go file"}, "tier": 1},
    {"input": "copy this code", "expected": {"action": "copy", "path": "this code"}, "tier": 1},
    {"input": "copy that document", "expected": {"action": "copy", "path": "that document"}, "tier": 1},
    {"input": "copy the excerpt", "expected": {"action": "copy", "path": "the excerpt"}, "tier": 1},
    {"input": "copy the result", "expected": {"action": "copy", "path": "the result"}, "tier": 1},
    {"input": "copy the file", "expected": {"action": "copy", "path": "the file"}, "tier": 1},
    {"input": "copy the match", "expected": {"action": "copy", "path": "the match"}, "tier": 1},
])


# ==============================================================================
# HIJACK SUITE (Tier 2 conversational cases containing fast-path verbs)
# ==============================================================================

hijack = [
    # 1. open hijacks
    {"input": "keep an open mind about the architecture", "expected": {"action": "excerpt", "query": "architecture"}, "tier": 2},
    {"input": "when did the store open for business", "expected": {"action": "excerpt", "query": "store open"}, "tier": 2},
    {"input": "open the pod bay doors hal", "expected": {"action": "excerpt", "query": "pod bay doors hal"}, "tier": 2},
    {"input": "open a new issue about the database crash", "expected": {"action": "excerpt", "query": "database crash issue"}, "tier": 2},
    {"input": "open up the discussion on pull requests", "expected": {"action": "excerpt", "query": "discussion pull requests"}, "tier": 2},
    {"input": "open sesame and show all secrets", "expected": {"action": "excerpt", "query": "secrets"}, "tier": 2},
    {"input": "open questions from yesterday meeting", "expected": {"action": "excerpt", "query": "meeting questions"}, "tier": 2},
    {"input": "open your mind to modern backend design", "expected": {"action": "excerpt", "query": "backend design"}, "tier": 2},
    {"input": "open up ticket 123 for discussion", "expected": {"action": "excerpt", "query": "ticket 123"}, "tier": 2},

    # 2. read hijacks
    {"input": "I need to read between the lines on this PR", "expected": {"action": "excerpt", "query": "PR"}, "tier": 2},
    {"input": "did anyone read the memo about postgres", "expected": {"action": "excerpt", "query": "postgres memo"}, "tier": 2},
    {"input": "read between the lines on this PR", "expected": {"action": "excerpt", "query": "PR"}, "tier": 2},
    {"input": "read me the latest deployment error", "expected": {"action": "read", "query": "deployment error"}, "tier": 2},
    {"input": "read the memo about postgres migration", "expected": {"action": "read", "query": "postgres migration memo"}, "tier": 2},
    {"input": "read through the comments on ticket 404", "expected": {"action": "read", "query": "comments ticket 404"}, "tier": 2},
    {"input": "read aloud the summary of the meeting", "expected": {"action": "read", "query": "meeting summary"}, "tier": 2},
    {"input": "read all about our release plans", "expected": {"action": "excerpt", "query": "release plans"}, "tier": 2},

    # 3. show hijacks
    {"input": "show me what's in the docker file", "expected": {"action": "read", "query": "docker"}, "tier": 2},
    {"input": "can you show us the connection string", "expected": {"action": "excerpt", "query": "connection string"}, "tier": 2},
    {"input": "show us how to reproduce the bug", "expected": {"action": "excerpt", "query": "reproduce bug"}, "tier": 2},
    {"input": "show the team the latest benchmark results", "expected": {"action": "read", "query": "benchmark results"}, "tier": 2},
    {"input": "show some examples of error handling", "expected": {"action": "excerpt", "query": "error handling examples"}, "tier": 2},
    {"input": "show all details regarding the outage", "expected": {"action": "excerpt", "query": "outage details"}, "tier": 2},
    {"input": "show how the authentication flow works", "expected": {"action": "excerpt", "query": "authentication flow"}, "tier": 2},

    # 4. cat hijacks
    {"input": "my cat is sitting on my keyboard", "expected": {"action": "locate", "query": "cat keyboard"}, "tier": 2},
    {"input": "cat videos are distracting me from the bug", "expected": {"action": "locate", "query": "cat videos bug"}, "tier": 2},
    {"input": "cat photos are stored in the cloud", "expected": {"action": "locate", "query": "cat photos cloud"}, "tier": 2},
    {"input": "cat got your tongue during the standup", "expected": {"action": "excerpt", "query": "standup"}, "tier": 2},
    {"input": "cat food receipt from last week", "expected": {"action": "locate", "query": "cat food receipt"}, "tier": 2},
    {"input": "cat is not my favorite animal", "expected": {"action": "excerpt", "query": "favorite animal"}, "tier": 2},
    {"input": "cat and dog adoption guidelines", "expected": {"action": "locate", "query": "adoption guidelines"}, "tier": 2},

    # 5. ls hijacks
    {"input": "is there any ls alias for kubernetes", "expected": {"action": "excerpt", "query": "kubernetes ls alias"}, "tier": 2},
    {"input": "ls alias for kubernetes cluster", "expected": {"action": "excerpt", "query": "kubernetes cluster ls alias"}, "tier": 2},
    {"input": "ls command options in bash", "expected": {"action": "excerpt", "query": "bash ls command options"}, "tier": 2},
    {"input": "ls flags for sorting by time", "expected": {"action": "excerpt", "query": "ls flags sorting time"}, "tier": 2},
    {"input": "ls output formatting with colors", "expected": {"action": "excerpt", "query": "ls output formatting colors"}, "tier": 2},
    {"input": "ls tricks for searching directories", "expected": {"action": "excerpt", "query": "ls searching directories"}, "tier": 2},

    # 6. list hijacks
    {"input": "what's the list of dependencies for this project", "expected": {"action": "excerpt", "query": "project dependencies"}, "tier": 2},
    {"input": "list out the top reasons for adopting rust", "expected": {"action": "excerpt", "query": "reasons adopting rust"}, "tier": 2},
    {"input": "list all active database connections", "expected": {"action": "excerpt", "query": "active database connections"}, "tier": 2},
    {"input": "list every contributor to the repository", "expected": {"action": "excerpt", "query": "repository contributors"}, "tier": 2},
    {"input": "list things we need to fix before release", "expected": {"action": "excerpt", "query": "fix before release"}, "tier": 2},
    {"input": "list reasons why the query failed", "expected": {"action": "excerpt", "query": "query failed reasons"}, "tier": 2},

    # 7. head hijacks
    {"input": "head straight to the database section", "expected": {"action": "excerpt", "query": "database"}, "tier": 2},
    {"input": "what is the head of the marketing department", "expected": {"action": "excerpt", "query": "head marketing department"}, "tier": 2},
    {"input": "head over to the settings page", "expected": {"action": "excerpt", "query": "settings page"}, "tier": 2},
    {"input": "head first into the codebase refactor", "expected": {"action": "excerpt", "query": "codebase refactor"}, "tier": 2},
    {"input": "head 10 reasons why we use go", "expected": {"action": "excerpt", "query": "reasons why we use go"}, "tier": 2},
    {"input": "head -5 steps to reproduce the issue", "expected": {"action": "excerpt", "query": "steps to reproduce issue"}, "tier": 2},
    {"input": "head 20 tips for writing clean code", "expected": {"action": "excerpt", "query": "tips writing clean code"}, "tier": 2},
    {"input": "head 5 items on the roadmap", "expected": {"action": "excerpt", "query": "roadmap items"}, "tier": 2},
    {"input": "head 100 ways to optimize database queries", "expected": {"action": "excerpt", "query": "optimize database queries"}, "tier": 2},

    # 8. tail hijacks
    {"input": "tail end of the meeting notes from yesterday", "expected": {"action": "read", "query": "meeting notes yesterday"}, "tier": 2},
    {"input": "tail call optimization in the compiler", "expected": {"action": "excerpt", "query": "tail call optimization compiler"}, "tier": 2},
    {"input": "tail latency issues with the redis cache", "expected": {"action": "excerpt", "query": "redis cache tail latency"}, "tier": 2},
    {"input": "tail chasing in the planning meeting", "expected": {"action": "excerpt", "query": "planning meeting"}, "tier": 2},
    {"input": "tail 5 items on the todo list", "expected": {"action": "excerpt", "query": "todo list items"}, "tier": 2},
    {"input": "tail -10 questions from the survey", "expected": {"action": "excerpt", "query": "survey questions"}, "tier": 2},
    {"input": "tail 100 things I learned this year", "expected": {"action": "excerpt", "query": "things learned this year"}, "tier": 2},
    {"input": "tail 3 comments on the pull request", "expected": {"action": "excerpt", "query": "pull request comments"}, "tier": 2},
    {"input": "tail 50 problems with our legacy stack", "expected": {"action": "excerpt", "query": "legacy stack problems"}, "tier": 2},

    # 9. find hijacks
    {"input": "find out what I wrote about docker", "expected": {"action": "excerpt", "query": "docker"}, "tier": 2},
    {"input": "I can't find the motivation to work on this", "expected": {"action": "locate", "query": "motivation"}, "tier": 2},
    {"input": "find the auth middleware file", "expected": {"action": "locate", "query": "auth middleware"}, "tier": 2},
    {"input": "find where we handle network timeouts", "expected": {"action": "locate", "query": "network timeouts"}, "tier": 2},
    {"input": "find information about the retirement plan", "expected": {"action": "excerpt", "query": "retirement plan"}, "tier": 2},
    {"input": "find someone who knows about kubernetes", "expected": {"action": "excerpt", "query": "kubernetes expert"}, "tier": 2},
    {"input": "find why the build is failing on ci", "expected": {"action": "excerpt", "query": "build failing ci"}, "tier": 2},
    {"input": "find all references to the legacy API", "expected": {"action": "locate", "query": "legacy API references"}, "tier": 2},
    {"input": "find the bug causing memory leaks", "expected": {"action": "locate", "query": "memory leaks bug"}, "tier": 2},

    # 10. copy hijacks
    {"input": "please don't copy my homework", "expected": {"action": "excerpt", "query": "homework"}, "tier": 2},
    {"input": "make a copy of the backup notes", "expected": {"action": "locate", "query": "backup notes"}, "tier": 2},
    {"input": "copy this text here", "expected": {"action": "copy", "query": "text here"}, "tier": 2},
    {"input": "copy the connection string to clipboard", "expected": {"action": "copy", "query": "connection string"}, "tier": 2},
    {"input": "copy and paste the error message", "expected": {"action": "copy", "query": "error message"}, "tier": 2},
    {"input": "copy my homework from yesterday", "expected": {"action": "copy", "query": "homework yesterday"}, "tier": 2},
    {"input": "copy everything we discussed earlier", "expected": {"action": "copy", "query": "discussed earlier"}, "tier": 2},
    {"input": "copy that into the email draft", "expected": {"action": "copy", "query": "email draft"}, "tier": 2},
    {"input": "copy all files to backup location", "expected": {"action": "copy", "query": "backup location"}, "tier": 2},
    {"input": "copy the database password for staging", "expected": {"action": "copy", "query": "staging database password"}, "tier": 2},
]
cases.extend(hijack)

# ==============================================================================
# Tier 2 - Ambiguous, edge cases, locators, excerpts, typos
# ==============================================================================

tier2_locate = [
    "where is my resume",
    "the docker notes file",
    "the kubernetes guide",
    "what folder is the frontend app in",
    "wher is the backend config", # typo
    "locate the postgres connection details",
    "where did i put the aws credentials",
    "the file with all the regex patterns",
    "search for the main entrypoint",
    "i need the project roadmap",
    "bring up the deployment script",
    "where's the test data directory",
    "locat the python environment setup", # typo
    "the design document for v2",
]
for q in tier2_locate:
    cases.append({"input": q, "expected": {"action": "locate", "query": q}, "tier": 2})

tier2_excerpt = [
    "what's my postgres connection string",
    "how do we handle the retry backoff",
    "what did the meeting notes say about the deadline",
    "explain the architecture of the router module",
    "who is responsible for the database migration",
    "what are the steps to deploy to staging",
    "how to configure the nginx reverse proxy",
    "what was the error code for connection refused",
    "why is the build failing on github actions",
    "what is the maximum file size for uploads",
    "whos the lead for the mobile app", # typo
    "what did we decide on the new caching strategy",
    "give me a summary of the latest release notes",
    "what are the main features of the new dashboard",
]
for q in tier2_excerpt:
    cases.append({"input": q, "expected": {"action": "excerpt", "query": q}, "tier": 2})

tier2_read = [
    "let me see the whole config file",
    "give me the contents of the access log",
    "print out the setup script",
    "i want to read the entire policy document",
    "display the whole error log",
    "let me read the instructions for the assignment",
    "gimme the full source code for the server", # typo/slang
    "i need to see all of the main.go file",
    "dump the contents of the database schema",
    "show all the text in the readme",
]
for q in tier2_read:
    cases.append({"input": q, "expected": {"action": "read", "query": q}, "tier": 2})

tier2_open = [
    "let's open the test runner",
    "pop open the main configuration file",
    "i want to open the database schema",
    "fire up the deployment script",
    "let me edit the nginx config",
    "i need to open the backend code",
    "launch the frontend app file",
    "open up the markdown notes",
    "i wanna open the sql dump",
    "pls open the python script",
]
for q in tier2_open:
    cases.append({"input": q, "expected": {"action": "open", "query": q}, "tier": 2})

tier2_copy = [
    "yank the regex pattern",
    "put the api key on my clipboard",
    "copy the whole setup block",
    "i need the docker command copied",
    "grab the ssh key for me",
    "copy the test coverage results",
    "save the error message to clipboard",
    "copy the url from the notes",
    "put the password in the clipboard",
]
for q in tier2_copy:
    cases.append({"input": q, "expected": {"action": "copy", "query": q}, "tier": 2})

tier2_list = [
    "what's in ~/projects",
    "show me the files in the temp directory",
    "list everything under the www folder",
    "what directories are in the src folder",
    "give me a listing of the assets dir",
    "what files are sitting in downloads",
    "enumerate the content of the bin folder",
    "show me all items in the repository root",
    "whats in the dist folder", # typo
    "list out the contents of the node_modules",
]
for q in tier2_list:
    cases.append({"input": q, "expected": {"action": "list", "query": q}, "tier": 2})

more_mixed = [
    ("what did I say about the auth token", "excerpt"),
    ("find the main css file", "locate"),
    ("how do i restart the service", "excerpt"),
    ("open the kubernetes yaml", "open"),
    ("read the whole tutorial", "read"),
    ("what's inside the config folder", "list"),
    ("yank the deployment script", "copy"),
    ("where did i save the logo", "locate"),
    ("how long is the timeout", "excerpt"),
    ("whats the max token limit", "excerpt"),
    ("i want to open the auth module", "open"),
    ("read the error trace", "read"),
    ("copy the stack trace", "copy"),
    ("list the test files", "list"),
    ("find the python virtual env", "locate"),
    ("what is the best practice for error handling", "excerpt"),
    ("show me the entire guide", "read"),
    ("i need to open the controller", "open"),
    ("where is the router defined", "locate"),
    ("what does the parser do", "excerpt"),
    ("copy the setup instructions", "copy"),
    ("list everything in the docs", "list"),
    ("find the utility functions", "locate"),
    ("how to mock the database", "excerpt"),
    ("open the test file for the router", "open"),
    ("let me read the contributing guidelines", "read"),
    ("copy the license text", "copy"),
    ("what are the folders here", "list"),
    ("where is the manifest file", "locate"),
    ("how do i build the project", "excerpt"),
    ("read the build output log", "read"),
    ("open the makefile", "open"),
    ("copy the build command", "copy"),
    ("list the generated artifacts", "list"),
    ("find the executable", "locate"),
    ("what is the version number", "excerpt"),
    ("show me the whole changelog", "read"),
    ("open the issue template", "open"),
    ("copy the pull request body", "copy"),
    ("list the open bugs", "list"),
    ("find the bug report", "locate"),
    ("how to fix the memory leak", "excerpt"),
    ("read the core dump", "read"),
    ("open the memory profiler output", "open"),
    ("copy the leak trace", "copy"),
    ("list the profiling results", "list"),
    ("find the heap snapshot", "locate"),
    ("what is the garbage collection overhead", "excerpt"),
    ("show me the complete gc log", "read"),
    ("open the performance tuning guide", "open"),
]

for q, a in more_mixed:
    cases.append({"input": q, "expected": {"action": a, "query": q}, "tier": 2})

# Write to jsonl
with open("testdata/intent_eval.jsonl", "w") as f:
    for c in cases:
        f.write(json.dumps(c) + "\n")
