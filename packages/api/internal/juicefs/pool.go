// Package juicefs provides a connection pool for JuiceFS clients.
// Clients are cached per-volume and reused to avoid repeated initialization.
package juicefs

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Pool manages a pool of JuiceFS clients, one per volume.
// Clients are cached and reused to avoid repeated initialization.
type Pool struct {
	config Config

	mu      sync.RWMutex
	clients map[string]*pooledClient

	// Idle timeout after which clients are closed
	idleTimeout time.Duration
}

type pooledClient struct {
	client   *Client
	lastUsed time.Time
}

// NewPool creates a new client pool with the given configuration.
func NewPool(config Config) *Pool {
	p := &Pool{
		config:      config,
		clients:     make(map[string]*pooledClient),
		idleTimeout: 5 * time.Minute,
	}

	// Start background cleanup goroutine
	go p.cleanupLoop()

	return p
}

// Get returns a client for the given volume, creating one if needed.
// The redisDB parameter is deprecated and ignored (kept for API compatibility).
func (p *Pool) Get(ctx context.Context, volumeID string, _ int32) (*Client, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Check for existing client
	if pc, ok := p.clients[volumeID]; ok {
		pc.lastUsed = time.Now()
		return pc.client, nil
	}

	// Create new client
	client, err := NewClient(volumeID, 0, p.config)
	if err != nil {
		return nil, fmt.Errorf("create client for volume %s: %w", volumeID, err)
	}

	p.clients[volumeID] = &pooledClient{
		client:   client,
		lastUsed: time.Now(),
	}

	return client, nil
}

// Config returns the pool's configuration.
func (p *Pool) Config() Config {
	return p.config
}

// Close closes all clients in the pool.
func (p *Pool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	var errs []error
	for volumeID, pc := range p.clients {
		if err := pc.client.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close client for %s: %w", volumeID, err))
		}
	}
	p.clients = make(map[string]*pooledClient)

	if len(errs) > 0 {
		return fmt.Errorf("errors closing clients: %v", errs)
	}
	return nil
}

// cleanupLoop periodically removes idle clients.
func (p *Pool) cleanupLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		p.cleanup()
	}
}

func (p *Pool) cleanup() {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	for volumeID, pc := range p.clients {
		if now.Sub(pc.lastUsed) > p.idleTimeout {
			pc.client.Close()
			delete(p.clients, volumeID)
		}
	}
}
