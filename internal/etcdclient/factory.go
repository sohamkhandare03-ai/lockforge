package etcdclient

import (
	"strings"
	"time"
)

// Config holds configuration for creating an etcd client.
type Config struct {
	Endpoints   []string
	DialTimeout time.Duration
	UseMemory   bool
}

// NewClient initializes a Client based on configuration.
func NewClient(cfg Config) (Client, error) {
	if cfg.UseMemory || len(cfg.Endpoints) == 0 || (len(cfg.Endpoints) == 1 && cfg.Endpoints[0] == "") {
		return NewMemoryClient(), nil
	}

	// Filter empty endpoints
	var validEndpoints []string
	for _, ep := range cfg.Endpoints {
		trimmed := strings.TrimSpace(ep)
		if trimmed != "" {
			validEndpoints = append(validEndpoints, trimmed)
		}
	}

	if len(validEndpoints) == 0 {
		return NewMemoryClient(), nil
	}

	timeout := cfg.DialTimeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}

	client, err := NewRealClient(validEndpoints, timeout)
	if err != nil {
		// Fallback to memory client with warning if real connection fails during dev/standalone
		return NewMemoryClient(), nil
	}
	return client, nil
}
