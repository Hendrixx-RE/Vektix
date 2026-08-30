#!/usr/bin/env bash
set -euo pipefail

echo "Building binaries..."
go build -o bin/server ./cmd/server
go build -o bin/cli ./cmd/cli

echo "Running static checks..."
go vet ./...

echo "Packaging docker build..."
docker build -t app:latest .
