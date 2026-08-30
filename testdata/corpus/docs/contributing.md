# Contributing Guide

## Development Setup

1. Clone the repository and install Go 1.23+.
2. Run unit tests before submitting PRs:
   ```bash
   go test ./... -race -count=1
   ```
3. Run linting:
   ```bash
   golangci-lint run ./...
   ```

## Code Style

- Keep functions focused and deterministic.
- Preserve test coverage and avoid global state.
- Ensure all public symbols have doc comments.
