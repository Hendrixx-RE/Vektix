import json
import random

# Base cases
cases = []

# Tier 1 cases
# open
cases.extend([
    {"input": "open main.go", "expected": {"action": "open", "path": "main.go"}, "tier": 1},
    {"input": "open /etc/hosts", "expected": {"action": "open", "path": "/etc/hosts"}, "tier": 1},
    {"input": "open src/config.json", "expected": {"action": "open", "path": "src/config.json"}, "tier": 1},
    {"input": "open README.md", "expected": {"action": "open", "path": "README.md"}, "tier": 1},
])
# read
cases.extend([
    {"input": "read server.go", "expected": {"action": "read", "path": "server.go"}, "tier": 1},
    {"input": "show logs/app.log", "expected": {"action": "read", "path": "logs/app.log"}, "tier": 1},
    {"input": "cat internal/util.go", "expected": {"action": "read", "path": "internal/util.go"}, "tier": 1},
    {"input": "head 30 README.md", "expected": {"action": "read", "path": "README.md", "lines": "30"}, "tier": 1},
    {"input": "tail -50 temp.txt", "expected": {"action": "read", "path": "temp.txt", "lines": "-50"}, "tier": 1},
])
# locate
cases.extend([
    {"input": "find *.go", "expected": {"action": "locate", "query": "*.go"}, "tier": 1},
    {"input": "find src/*.ts", "expected": {"action": "locate", "query": "src/*.ts"}, "tier": 1},
    {"input": "find ?est.go", "expected": {"action": "locate", "query": "?est.go"}, "tier": 1},
])
# copy
cases.extend([
    {"input": "copy it", "expected": {"action": "copy", "path": "it"}, "tier": 1},
    {"input": "copy #2", "expected": {"action": "copy", "path": "#2"}, "tier": 1},
    {"input": "copy the first one", "expected": {"action": "copy", "path": "the first one"}, "tier": 1},
    {"input": "copy config.yaml", "expected": {"action": "copy", "path": "config.yaml"}, "tier": 1},
])
# list
cases.extend([
    {"input": "ls /var/log", "expected": {"action": "list", "path": "/var/log"}, "tier": 1},
    {"input": "list ~/.config", "expected": {"action": "list", "path": "~/.config"}, "tier": 1},
    {"input": "ls testdata/", "expected": {"action": "list", "path": "testdata/"}, "tier": 1},
])

# HIJACK SUITE (Tier 2 conversational cases containing fast-path verbs)
hijack = [
    {"input": "keep an open mind about the architecture", "expected": {"action": "excerpt", "query": "architecture"}, "tier": 2},
    {"input": "when did the store open for business", "expected": {"action": "excerpt", "query": "store open"}, "tier": 2},
    {"input": "I need to read between the lines on this PR", "expected": {"action": "excerpt", "query": "PR"}, "tier": 2},
    {"input": "did anyone read the memo about postgres", "expected": {"action": "excerpt", "query": "postgres memo"}, "tier": 2},
    {"input": "show me what's in the docker file", "expected": {"action": "read", "query": "docker"}, "tier": 2},
    {"input": "can you show us the connection string", "expected": {"action": "excerpt", "query": "connection string"}, "tier": 2},
    {"input": "my cat is sitting on my keyboard", "expected": {"action": "locate", "query": "cat keyboard"}, "tier": 2},
    {"input": "cat videos are distracting me from the bug", "expected": {"action": "locate", "query": "cat videos bug"}, "tier": 2},
    {"input": "is there any ls alias for kubernetes", "expected": {"action": "excerpt", "query": "kubernetes ls alias"}, "tier": 2},
    {"input": "what's the list of dependencies for this project", "expected": {"action": "excerpt", "query": "project dependencies"}, "tier": 2},
    {"input": "list out the top reasons for adopting rust", "expected": {"action": "excerpt", "query": "reasons adopting rust"}, "tier": 2},
    {"input": "head straight to the database section", "expected": {"action": "excerpt", "query": "database"}, "tier": 2},
    {"input": "what is the head of the marketing department", "expected": {"action": "excerpt", "query": "head marketing department"}, "tier": 2},
    {"input": "tail end of the meeting notes from yesterday", "expected": {"action": "read", "query": "meeting notes yesterday"}, "tier": 2},
    {"input": "find out what I wrote about docker", "expected": {"action": "excerpt", "query": "docker"}, "tier": 2},
    {"input": "I can't find the motivation to work on this", "expected": {"action": "locate", "query": "motivation"}, "tier": 2},
    {"input": "please don't copy my homework", "expected": {"action": "excerpt", "query": "homework"}, "tier": 2},
    {"input": "make a copy of the backup notes", "expected": {"action": "locate", "query": "backup notes"}, "tier": 2},
]
cases.extend(hijack)

# Tier 2 - Ambiguous, edge cases, locators, excerpts, typos
tier2_locate = [
    "where is my resume",
    "the docker notes file",
    "the kubernetes guide",
    "what folder is the frontend app in",
    "wher is the backend config", # typo
    "locate the postgres connection details",
    "find the auth middleware file",
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
    "how does the authentication flow work",
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
    "copy the connection string to clipboard",
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
    "list everything under /var/www",
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

# generate more to reach ~150
# Currently we have ~100
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
