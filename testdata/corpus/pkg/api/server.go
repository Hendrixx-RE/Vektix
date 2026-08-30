package api

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// RateLimiter enforces request limits per client IP address.
type RateLimiter struct {
	requestsPerSec int
	visitors       map[string]int
	mu             sync.Mutex
}

// RateLimitMiddleware intercepts incoming requests and enforces rate limits.
func (rl *RateLimiter) RateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := r.RemoteAddr
		rl.mu.Lock()
		count := rl.visitors[ip]
		if count >= rl.requestsPerSec {
			rl.mu.Unlock()
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		rl.visitors[ip] = count + 1
		rl.mu.Unlock()
		next.ServeHTTP(w, r)
	})
}

// HTTPServer encapsulates the HTTP router and graceful shutdown lifecycle.
type HTTPServer struct {
	server *http.Server
}

// NewHTTPServer initializes standard routes and middleware.
func NewHTTPServer(addr string, mux *http.ServeMux) *HTTPServer {
	return &HTTPServer{
		server: &http.Server{
			Addr:         addr,
			Handler:      mux,
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 10 * time.Second,
		},
	}
}

// Start launches the HTTP server listening on the configured address.
func (s *HTTPServer) Start() error {
	fmt.Printf("HTTP server listening on %s\n", s.server.Addr)
	if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// Shutdown initiates graceful termination of active connections.
func (s *HTTPServer) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}
