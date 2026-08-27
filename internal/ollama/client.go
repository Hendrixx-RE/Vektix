package ollama

import (
	"net/http"
	"time"
)

// Client is an HTTP client for Ollama's local API.
type Client struct {
	baseURL           string
	httpClient        *http.Client
	embedTimeout      time.Duration
	intentTimeout     time.Duration
	streamIdleTimeout time.Duration
}

// Options configure the Ollama Client.
type Options struct {
	Host              string
	EmbedTimeout      time.Duration
	IntentTimeout     time.Duration
	StreamIdleTimeout time.Duration
}

// NewClient creates a new Client with the given options.
func NewClient(opts Options) *Client {
	if opts.Host == "" {
		opts.Host = "http://localhost:11434"
	}
	
	// Create a transport with standard timeouts but no overall timeout on the client
	// so that streaming can be handled via idle timeouts.
	transport := http.DefaultTransport.(*http.Transport).Clone()
	
	return &Client{
		baseURL:           opts.Host,
		httpClient:        &http.Client{Transport: transport},
		embedTimeout:      opts.EmbedTimeout,
		intentTimeout:     opts.IntentTimeout,
		streamIdleTimeout: opts.StreamIdleTimeout,
	}
}
