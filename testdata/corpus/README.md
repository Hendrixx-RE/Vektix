# Sample Corpus Project

A sample microservices backend with authentication, caching, metrics, and background worker queues.

## Quickstart

To run the application server locally:

```bash
go run ./cmd/server/main.go
```

To run diagnostics and maintenance tasks:

```bash
go run ./cmd/cli/main.go
```

## Features

- JWT Authentication and role-based access control.
- PostgreSQL connection pooling with exponential retry backoff.
- In-memory LRU cache with eviction policies.
- Background asynchronous task workers with dead-letter queue.
- Prometheus metrics collector and HTTP rate-limiting middleware.
- Kubernetes deployment with HorizontalPodAutoscaler.

## License

MIT License.
