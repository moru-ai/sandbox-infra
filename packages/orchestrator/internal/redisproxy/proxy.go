// Package redisproxy provides a TCP proxy for Redis that injects per-volume ACL credentials.
package redisproxy

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/moru-ai/sandbox-infra/packages/shared/pkg/logger"
)

const (
	// Port is the port the Redis proxy listens on in sandbox netns.
	Port = 5018
)

// Config holds the configuration for a Redis proxy instance.
type Config struct {
	// ListenAddr is the address to listen on (e.g., "10.12.0.1:5018").
	ListenAddr string

	// UpstreamURL is the Redis URL (e.g., "redis://10.0.0.1:6379" or "rediss://10.0.0.1:6379?insecure-skip-verify=true").
	// Supports redis:// (plain) and rediss:// (TLS) schemes.
	UpstreamURL string

	// RedisDB is the database number for key prefix isolation.
	// Used to construct username: db_{RedisDB}
	RedisDB int

	// Password is the per-volume password for Redis ACL authentication.
	Password string
}

// upstreamConfig holds parsed upstream connection configuration.
type upstreamConfig struct {
	host      string
	tlsConfig *tls.Config
}

// parseUpstreamURL parses the upstream URL and returns connection configuration.
func parseUpstreamURL(rawURL string) (*upstreamConfig, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse URL: %w", err)
	}

	host := u.Host
	if host == "" {
		return nil, fmt.Errorf("empty host in URL: %s", rawURL)
	}

	// Add default port if not specified
	if !strings.Contains(host, ":") {
		host = host + ":6379"
	}

	cfg := &upstreamConfig{
		host: host,
	}

	// Check scheme for TLS
	switch u.Scheme {
	case "redis":
		// Plain TCP, no TLS
		cfg.tlsConfig = nil
	case "rediss":
		// TLS enabled
		cfg.tlsConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
		// Check for insecure-skip-verify query param
		if u.Query().Get("insecure-skip-verify") == "true" {
			cfg.tlsConfig.InsecureSkipVerify = true
		}
	default:
		return nil, fmt.Errorf("unsupported scheme: %s (expected redis:// or rediss://)", u.Scheme)
	}

	return cfg, nil
}

// Proxy is a TCP proxy that injects Redis ACL authentication.
type Proxy struct {
	config   Config
	logger   logger.Logger
	listener net.Listener

	// upstream holds parsed upstream configuration (parsed once at start)
	upstream *upstreamConfig

	mu      sync.Mutex
	running bool
	ctx     context.Context
	cancel  context.CancelFunc
}

// New creates a new Redis proxy instance.
func New(cfg Config, logger logger.Logger) *Proxy {
	return &Proxy{
		config: cfg,
		logger: logger,
	}
}

// Start starts the proxy and blocks until the context is cancelled.
func (p *Proxy) Start(ctx context.Context) error {
	p.mu.Lock()
	if p.running {
		p.mu.Unlock()
		return fmt.Errorf("proxy already running")
	}
	p.running = true
	p.ctx, p.cancel = context.WithCancel(ctx)
	p.mu.Unlock()

	// Parse upstream URL once at startup
	var err error
	p.upstream, err = parseUpstreamURL(p.config.UpstreamURL)
	if err != nil {
		return fmt.Errorf("invalid upstream URL: %w", err)
	}

	// Create listener
	p.listener, err = net.Listen("tcp", p.config.ListenAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", p.config.ListenAddr, err)
	}

	p.logger.Info(ctx, "Redis proxy started",
		zap.String("addr", p.config.ListenAddr),
		zap.String("upstream", p.upstream.host),
		zap.Bool("tls", p.upstream.tlsConfig != nil),
		zap.Int("redisDb", p.config.RedisDB),
	)

	// Start shutdown goroutine
	go func() {
		<-p.ctx.Done()
		p.listener.Close()
	}()

	// Accept loop
	for {
		conn, err := p.listener.Accept()
		if err != nil {
			select {
			case <-p.ctx.Done():
				return nil
			default:
				p.logger.Error(ctx, "Redis proxy accept error", zap.Error(err))
				continue
			}
		}

		go p.handleConnection(conn)
	}
}

// Close stops the proxy.
func (p *Proxy) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cancel != nil {
		p.cancel()
	}
	if p.listener != nil {
		return p.listener.Close()
	}
	return nil
}

// handleConnection handles a single client connection.
func (p *Proxy) handleConnection(clientConn net.Conn) {
	defer clientConn.Close()
	ctx := p.ctx

	// Connect to upstream Redis
	var upstreamConn net.Conn
	var err error

	if p.upstream.tlsConfig != nil {
		dialer := &net.Dialer{Timeout: 10 * time.Second}
		upstreamConn, err = tls.DialWithDialer(dialer, "tcp", p.upstream.host, p.upstream.tlsConfig)
	} else {
		upstreamConn, err = net.DialTimeout("tcp", p.upstream.host, 10*time.Second)
	}
	if err != nil {
		p.logger.Error(ctx, "Redis proxy: failed to connect to upstream", zap.Error(err))
		return
	}
	defer upstreamConn.Close()

	// Authenticate with upstream using per-volume ACL credentials
	if err := p.authenticate(upstreamConn); err != nil {
		p.logger.Error(ctx, "Redis proxy: authentication failed", zap.Error(err))
		return
	}

	// Bidirectional copy
	done := make(chan struct{}, 2)

	go func() {
		io.Copy(upstreamConn, clientConn)
		done <- struct{}{}
	}()

	go func() {
		io.Copy(clientConn, upstreamConn)
		done <- struct{}{}
	}()

	// Wait for either direction to finish
	<-done
}

// authenticate sends the AUTH command to upstream Redis.
func (p *Proxy) authenticate(conn net.Conn) error {
	username := fmt.Sprintf("db_%d", p.config.RedisDB)

	// Build RESP AUTH command: *3\r\n$4\r\nAUTH\r\n${len(username)}\r\n{username}\r\n${len(password)}\r\n{password}\r\n
	authCmd := fmt.Sprintf("*3\r\n$4\r\nAUTH\r\n$%d\r\n%s\r\n$%d\r\n%s\r\n",
		len(username), username,
		len(p.config.Password), p.config.Password)

	// Send AUTH command
	if _, err := conn.Write([]byte(authCmd)); err != nil {
		return fmt.Errorf("write AUTH: %w", err)
	}

	// Read response
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	reader := bufio.NewReader(conn)
	response, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("read AUTH response: %w", err)
	}
	conn.SetReadDeadline(time.Time{}) // Clear deadline

	// Check response
	response = strings.TrimSpace(response)
	if !strings.HasPrefix(response, "+OK") {
		return fmt.Errorf("AUTH failed: %s", response)
	}

	return nil
}

// StartInNamespace starts the Redis proxy in the given network namespace.
// This is a helper that runs the proxy in a goroutine and returns immediately.
// Returns an error if the proxy fails to start (e.g., listen fails).
func StartInNamespace(ctx context.Context, cfg Config, log logger.Logger) (*Proxy, error) {
	proxy := New(cfg, log)

	// Channel to receive startup error
	errCh := make(chan error, 1)

	go func() {
		err := proxy.Start(ctx)
		if err != nil {
			errCh <- err
		}
		// If Start returns nil, proxy is shutting down normally
	}()

	// Wait for proxy to start or fail
	select {
	case err := <-errCh:
		return nil, fmt.Errorf("proxy startup failed: %w", err)
	case <-time.After(100 * time.Millisecond):
		// Proxy started successfully (blocking in accept loop)
		return proxy, nil
	}
}
