// Package redisproxy provides a TCP proxy for Redis that injects per-volume ACL credentials.
package redisproxy

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
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

	// UpstreamAddr is the Redis cluster endpoint (e.g., "10.0.0.1:6379").
	UpstreamAddr string

	// RedisDB is the database number for key prefix isolation.
	// Used to construct username: db_{RedisDB}
	RedisDB int

	// Password is the per-volume password for Redis ACL authentication.
	Password string

	// TLSConfig is the TLS configuration for upstream connection.
	// If nil, TLS is disabled.
	TLSConfig *tls.Config
}

// Proxy is a TCP proxy that injects Redis ACL authentication.
type Proxy struct {
	config   Config
	logger   logger.Logger
	listener net.Listener

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

	// Create listener
	var err error
	p.listener, err = net.Listen("tcp", p.config.ListenAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", p.config.ListenAddr, err)
	}

	p.logger.Info(ctx, "Redis proxy started",
		zap.String("addr", p.config.ListenAddr),
		zap.String("upstream", p.config.UpstreamAddr),
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

	if p.config.TLSConfig != nil {
		dialer := &net.Dialer{Timeout: 10 * time.Second}
		upstreamConn, err = tls.DialWithDialer(dialer, "tcp", p.config.UpstreamAddr, p.config.TLSConfig)
	} else {
		upstreamConn, err = net.DialTimeout("tcp", p.config.UpstreamAddr, 10*time.Second)
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
func StartInNamespace(ctx context.Context, cfg Config, log logger.Logger) (*Proxy, error) {
	proxy := New(cfg, log)

	go func() {
		if err := proxy.Start(ctx); err != nil {
			log.Error(ctx, "Redis proxy error", zap.Error(err))
		}
	}()

	// Give the proxy a moment to start
	time.Sleep(10 * time.Millisecond)

	return proxy, nil
}
