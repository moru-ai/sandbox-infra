package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/moru-ai/sandbox-infra/packages/shared/pkg/consts"
	"github.com/moru-ai/sandbox-infra/packages/shared/pkg/logger"
	"github.com/moru-ai/sandbox-infra/packages/shared/pkg/telemetry"
	"github.com/moru-ai/sandbox-infra/packages/shared/pkg/utils"
)

const (
	loopDelay = 5 * time.Millisecond
)

// doRequestWithInfiniteRetries does a request with infinite retries until the context is done.
// The parent context should have a deadline or a timeout.
func doRequestWithInfiniteRetries(
	ctx context.Context,
	method,
	address string,
	accessToken *string,
	envdInitRequestTimeout time.Duration,
	envVars map[string]string,
	sandboxID,
	envdVersion,
	hyperloopIP string,
	defaultUser *string,
	defaultWorkdir *string,
	volume *InitVolumeConfig,
) (*http.Response, int64, error) {
	requestCount := int64(0)
	for {
		now := time.Now()

		jsonBody := &PostInitJSONBody{
			EnvVars:        &envVars,
			HyperloopIP:    &hyperloopIP,
			AccessToken:    accessToken,
			Timestamp:      &now,
			DefaultUser:    defaultUser,
			DefaultWorkdir: defaultWorkdir,
			Volume:         volume,
		}

		body, err := json.Marshal(jsonBody)
		if err != nil {
			return nil, requestCount, err
		}

		requestCount++
		reqCtx, cancel := context.WithTimeout(ctx, envdInitRequestTimeout)
		request, err := http.NewRequestWithContext(reqCtx, method, address, bytes.NewReader(body))
		if err != nil {
			cancel()

			return nil, requestCount, err
		}

		// make sure request to already authorized envd will not fail
		// this can happen in sandbox resume and in some edge cases when previous request was success, but we continued
		if accessToken != nil {
			request.Header.Set("X-Access-Token", *accessToken)
		}

		response, err := sandboxHttpClient.Do(request)
		cancel()

		if err == nil {
			return response, requestCount, nil
		}

		logger.L().Warn(ctx, "failed to do request to envd, retrying", logger.WithSandboxID(sandboxID), logger.WithEnvdVersion(envdVersion), zap.Int64("timeout_ms", envdInitRequestTimeout.Milliseconds()), zap.Error(err))

		select {
		case <-ctx.Done():
			return nil, requestCount, fmt.Errorf("%w with cause: %w", ctx.Err(), context.Cause(ctx))
		case <-time.After(loopDelay):
		}
	}
}

type PostInitJSONBody struct {
	EnvVars        *map[string]string    `json:"envVars"`
	AccessToken    *string               `json:"accessToken,omitempty"`
	HyperloopIP    *string               `json:"hyperloopIP,omitempty"`
	Timestamp      *time.Time            `json:"timestamp,omitempty"`
	DefaultUser    *string               `json:"defaultUser,omitempty"`
	DefaultWorkdir *string               `json:"defaultWorkdir,omitempty"`
	Volume         *InitVolumeConfig     `json:"volume,omitempty"`
}

// InitVolumeConfig is the volume configuration sent to envd in the /init request.
type InitVolumeConfig struct {
	// VolumeID is the volume identifier (e.g., "vol_abc123").
	VolumeID string `json:"volumeId"`
	// MountPath is the path where the volume should be mounted (e.g., "/workspace/data").
	MountPath string `json:"mountPath"`
	// GCSBucket is the GCS bucket for volume data.
	GCSBucket string `json:"gcsBucket"`
	// GCSToken is the downscoped OAuth2 access token for GCS.
	GCSToken string `json:"gcsToken"`
	// GCSTokenExpiry is the Unix timestamp when the token expires.
	GCSTokenExpiry int64 `json:"gcsTokenExpiry"`
}

func (s *Sandbox) initEnvd(ctx context.Context) (e error) {
	ctx, span := tracer.Start(ctx, "envd-init", trace.WithAttributes(telemetry.WithEnvdVersion(s.Config.Envd.Version)))
	defer func() {
		if e != nil {
			span.SetStatus(codes.Error, e.Error())
		}

		span.End()
	}()

	attributes := []attribute.KeyValue{telemetry.WithEnvdVersion(s.Config.Envd.Version), attribute.Int64("timeout_ms", s.internalConfig.EnvdInitRequestTimeout.Milliseconds())}
	attributesFail := append(attributes, attribute.Bool("success", false))
	attributesSuccess := append(attributes, attribute.Bool("success", true))

	hyperloopIP := s.Slot.HyperloopIPString()
	address := fmt.Sprintf("http://%s:%d/init", s.Slot.HostIPString(), consts.DefaultEnvdServerPort)

	response, count, err := doRequestWithInfiniteRetries(
		ctx,
		http.MethodPost,
		address,
		s.Config.Envd.AccessToken,
		s.internalConfig.EnvdInitRequestTimeout,
		s.Config.Envd.Vars,
		s.Runtime.SandboxID,
		s.Config.Envd.Version,
		hyperloopIP,
		s.Config.Envd.DefaultUser,
		s.Config.Envd.DefaultWorkdir,
		s.volumeInitConfig,
	)
	if err != nil {
		envdInitCalls.Add(ctx, count, metric.WithAttributes(attributesFail...))

		return fmt.Errorf("failed to init envd: %w", err)
	}

	if count > 1 {
		// Track failed envd init calls
		envdInitCalls.Add(ctx, count-1, metric.WithAttributes(attributesFail...))
	}

	// Track successful envd init
	envdInitCalls.Add(ctx, 1, metric.WithAttributes(attributesSuccess...))

	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return fmt.Errorf("failed to read envd init response body: %w", err)
	}

	if response.StatusCode != http.StatusNoContent {
		logger.L().Error(ctx, "envd init request failed",
			logger.WithSandboxID(s.Runtime.SandboxID),
			logger.WithEnvdVersion(s.Config.Envd.Version),
			zap.Int("status_code", response.StatusCode),
			zap.String("response_body", utils.Truncate(string(body), 100)),
		)

		return fmt.Errorf("unexpected status code: %d", response.StatusCode)
	}

	span.SetStatus(codes.Ok, fmt.Sprintf("envd init returned %d", response.StatusCode))

	return nil
}

const (
	// shutdownTimeout is the maximum time to wait for the envd shutdown endpoint.
	// JuiceFS has a 300MB write buffer that needs to be flushed.
	shutdownTimeout = 30 * time.Second
)

// callEnvdShutdown calls the envd shutdown endpoint to flush volume buffers.
// This should be called before terminating the sandbox to prevent data loss.
func (s *Sandbox) callEnvdShutdown(ctx context.Context) error {
	ctx, span := tracer.Start(ctx, "envd-shutdown")
	defer span.End()

	// Use a timeout for the shutdown call
	ctx, cancel := context.WithTimeout(ctx, shutdownTimeout)
	defer cancel()

	address := fmt.Sprintf("http://%s:%d/shutdown", s.Slot.HostIPString(), consts.DefaultEnvdServerPort)

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, address, nil)
	if err != nil {
		return fmt.Errorf("failed to create shutdown request: %w", err)
	}

	// Include access token if set
	if s.Config.Envd.AccessToken != nil {
		request.Header.Set("X-Access-Token", *s.Config.Envd.AccessToken)
	}

	response, err := sandboxHttpClient.Do(request)
	if err != nil {
		return fmt.Errorf("failed to call envd shutdown: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusNoContent && response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		return fmt.Errorf("envd shutdown returned status %d: %s", response.StatusCode, string(body))
	}

	logger.L().Info(ctx, "envd shutdown completed successfully",
		zap.String("sandbox_id", s.Runtime.SandboxID),
	)

	return nil
}
