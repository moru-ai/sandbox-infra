// Package gcsproxy provides a reverse proxy for GCS that injects credentials
// and validates path prefixes for volume isolation.
package gcsproxy

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"golang.org/x/oauth2/google"

	"github.com/moru-ai/sandbox-infra/packages/shared/pkg/logger"
)

const (
	// Port is the port the GCS proxy listens on in sandbox netns.
	Port = 5017

	gcsEndpoint = "https://storage.googleapis.com"
)

// Config holds the configuration for a GCS proxy instance.
type Config struct {
	// ListenAddr is the address to listen on (e.g., "10.12.0.1:5017").
	ListenAddr string

	// VolumeID is the volume ID for path validation (e.g., "vol_abc123").
	VolumeID string

	// Bucket is the GCS bucket name.
	Bucket string
}

// Proxy is an HTTP reverse proxy that injects GCS credentials.
type Proxy struct {
	config   Config
	logger   logger.Logger
	server   *http.Server
	listener net.Listener

	// tokenSource provides GCS access tokens via ADC.
	tokenSource *google.Credentials

	mu      sync.Mutex
	running bool
}

// New creates a new GCS proxy instance.
func New(cfg Config, logger logger.Logger) (*Proxy, error) {
	ctx := context.Background()

	// Get default credentials for GCS
	creds, err := google.FindDefaultCredentials(ctx, "https://www.googleapis.com/auth/devstorage.full_control")
	if err != nil {
		return nil, fmt.Errorf("get default credentials: %w", err)
	}

	return &Proxy{
		config:      cfg,
		logger:      logger,
		tokenSource: creds,
	}, nil
}

// Start starts the proxy and blocks until the context is cancelled.
func (p *Proxy) Start(ctx context.Context) error {
	p.mu.Lock()
	if p.running {
		p.mu.Unlock()
		return fmt.Errorf("proxy already running")
	}
	p.running = true
	p.mu.Unlock()

	// Create listener
	var err error
	p.listener, err = net.Listen("tcp", p.config.ListenAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", p.config.ListenAddr, err)
	}

	// Parse upstream URL
	upstream, err := url.Parse(gcsEndpoint)
	if err != nil {
		return fmt.Errorf("parse GCS endpoint: %w", err)
	}

	// Create reverse proxy
	reverseProxy := httputil.NewSingleHostReverseProxy(upstream)
	reverseProxy.Transport = &http.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
		MaxIdleConns:        100,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}

	// Wrap with path validation and credential injection
	handler := p.wrapHandler(reverseProxy)

	p.server = &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Start shutdown goroutine
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		p.server.Shutdown(shutdownCtx)
	}()

	p.logger.Info(ctx, "GCS proxy started",
		zap.String("addr", p.config.ListenAddr),
		zap.String("volumeId", p.config.VolumeID),
		zap.String("bucket", p.config.Bucket),
	)

	err = p.server.Serve(p.listener)
	if err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("serve: %w", err)
	}

	return nil
}

// Close stops the proxy.
func (p *Proxy) Close() error {
	if p.server != nil {
		return p.server.Close()
	}
	return nil
}

// wrapHandler wraps the reverse proxy with path validation and credential injection.
func (p *Proxy) wrapHandler(proxy *httputil.ReverseProxy) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// Validate path prefix
		if !p.isPathAllowed(r.URL.Path, r.URL.RawQuery) {
			p.logger.Warn(ctx, "GCS proxy: path not allowed",
				zap.String("path", r.URL.Path),
				zap.String("volumeId", p.config.VolumeID),
			)
			http.Error(w, "Forbidden: path not allowed for this volume", http.StatusForbidden)
			return
		}

		// Inject authorization header
		token, err := p.getToken(ctx)
		if err != nil {
			p.logger.Error(ctx, "GCS proxy: failed to get token", zap.Error(err))
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		r.Header.Set("Authorization", "Bearer "+token)

		// Update host header for GCS
		r.Host = "storage.googleapis.com"

		proxy.ServeHTTP(w, r)
	})
}

// isPathAllowed checks if the request path is for the allowed volume.
// GCS JSON API uses paths like:
// - GET /storage/v1/b/${bucket}/o/${object}?alt=media
// - POST /upload/storage/v1/b/${bucket}/o?name=${object}
// - GET /storage/v1/b/${bucket}/o?prefix=${prefix}
func (p *Proxy) isPathAllowed(path, query string) bool {
	// Volume prefix for path matching
	volumePrefix := p.config.VolumeID + "/"

	// Check object name in query parameters (for uploads and lists)
	if query != "" {
		values, err := url.ParseQuery(query)
		if err == nil {
			// Check "name" parameter (used in uploads)
			if name := values.Get("name"); name != "" {
				if !strings.HasPrefix(name, volumePrefix) {
					return false
				}
				return true
			}
			// Check "prefix" parameter (used in list operations)
			if prefix := values.Get("prefix"); prefix != "" {
				if !strings.HasPrefix(prefix, volumePrefix) {
					return false
				}
				return true
			}
		}
	}

	// Check object name in path (for downloads and deletes)
	// Path format: /storage/v1/b/{bucket}/o/{object}
	if strings.Contains(path, "/o/") {
		parts := strings.SplitN(path, "/o/", 2)
		if len(parts) == 2 {
			// URL-decode the object name
			objectName, err := url.PathUnescape(parts[1])
			if err != nil {
				return false
			}
			if !strings.HasPrefix(objectName, volumePrefix) {
				return false
			}
			return true
		}
	}

	// Allow bucket-level operations without object name
	// (like checking if bucket exists)
	if strings.Contains(path, "/b/"+p.config.Bucket) && !strings.Contains(path, "/o/") && query == "" {
		return true
	}

	return false
}

// getToken returns a valid GCS access token.
func (p *Proxy) getToken(ctx context.Context) (string, error) {
	token, err := p.tokenSource.TokenSource.Token()
	if err != nil {
		return "", err
	}
	return token.AccessToken, nil
}

// StartInNamespace starts the GCS proxy in the given network namespace.
// This is a helper that runs the proxy in a goroutine and returns immediately.
func StartInNamespace(ctx context.Context, cfg Config, log logger.Logger) (*Proxy, error) {
	proxy, err := New(cfg, log)
	if err != nil {
		return nil, err
	}

	go func() {
		if err := proxy.Start(ctx); err != nil {
			log.Error(ctx, "GCS proxy error", zap.Error(err))
		}
	}()

	// Give the proxy a moment to start
	time.Sleep(10 * time.Millisecond)

	return proxy, nil
}

// Copy is a simple bidirectional copy between two connections.
func Copy(dst, src io.ReadWriteCloser) {
	defer dst.Close()
	defer src.Close()
	io.Copy(dst, src)
}
