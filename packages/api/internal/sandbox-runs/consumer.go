package sandboxruns

import (
	"context"
	"encoding/json"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	sqlcdb "github.com/moru-ai/sandbox-infra/packages/db/client"
	"github.com/moru-ai/sandbox-infra/packages/db/queries"
	"github.com/moru-ai/sandbox-infra/packages/shared/pkg/events"
	"github.com/moru-ai/sandbox-infra/packages/shared/pkg/logger"
)

const (
	groupName = "api-sandbox-runs"
	batchSize = 100
	blockTime = 5 * time.Second
	claimTime = 5 * time.Minute
)

type Consumer struct {
	redis      redis.UniversalClient
	db         *sqlcdb.Client
	consumerID string
}

func NewConsumer(redisClient redis.UniversalClient, db *sqlcdb.Client) *Consumer {
	hostname, _ := os.Hostname()
	consumerID := hostname + "-" + time.Now().Format("20060102150405")

	return &Consumer{
		redis:      redisClient,
		db:         db,
		consumerID: consumerID,
	}
}

func (c *Consumer) Run(ctx context.Context) {
	logger.L().Info(ctx, "Starting sandbox runs consumer",
		zap.String("consumerID", c.consumerID),
		zap.String("group", groupName))

	// Create consumer group (idempotent)
	err := c.redis.XGroupCreateMkStream(ctx, events.SandboxEventsStreamName, groupName, "0").Err()
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		logger.L().Error(ctx, "Failed to create consumer group", zap.Error(err))
		return
	}

	for {
		select {
		case <-ctx.Done():
			logger.L().Info(ctx, "Sandbox runs consumer stopping")
			return
		default:
			c.processBatch(ctx)
		}
	}
}

func (c *Consumer) processBatch(ctx context.Context) {
	// Read new messages
	streams, err := c.redis.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    groupName,
		Consumer: c.consumerID,
		Streams:  []string{events.SandboxEventsStreamName, ">"},
		Count:    batchSize,
		Block:    blockTime,
	}).Result()
	if err != nil {
		if err != redis.Nil {
			logger.L().Error(ctx, "Failed to read from stream", zap.Error(err))
		}
		return
	}

	for _, stream := range streams {
		for _, msg := range stream.Messages {
			if err := c.processMessage(ctx, msg); err != nil {
				logger.L().Error(ctx, "Failed to process message",
					zap.String("messageID", msg.ID),
					zap.Error(err))
				continue // Don't ACK, will be redelivered
			}

			// ACK only on success
			c.redis.XAck(ctx, events.SandboxEventsStreamName, groupName, msg.ID)
		}
	}

	// Claim old pending messages from crashed consumers
	c.claimPendingMessages(ctx)
}

func (c *Consumer) processMessage(ctx context.Context, msg redis.XMessage) error {
	payload, ok := msg.Values["payload"].(string)
	if !ok {
		return nil // Skip malformed messages
	}

	var event events.SandboxEvent
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		return nil // Skip unparseable messages
	}

	return c.handleEvent(ctx, event)
}

func (c *Consumer) handleEvent(ctx context.Context, event events.SandboxEvent) error {
	switch event.Type {
	case events.SandboxCreatedEvent:
		return c.handleCreated(ctx, event)
	case events.SandboxKilledEvent:
		return c.handleKilled(ctx, event)
	case events.SandboxPausedEvent:
		return c.handlePaused(ctx, event)
	case events.SandboxResumedEvent:
		return c.handleResumed(ctx, event)
	}
	return nil
}

func (c *Consumer) handleCreated(ctx context.Context, event events.SandboxEvent) error {
	logger.L().Debug(ctx, "Processing sandbox created event",
		logger.WithSandboxID(event.SandboxID),
		logger.WithTemplateID(event.SandboxTemplateID))

	buildID := &event.SandboxBuildID
	if event.SandboxBuildID == "" {
		buildID = nil
	}

	_, err := c.db.CreateSandboxRun(ctx, queries.CreateSandboxRunParams{
		SandboxID:  event.SandboxID,
		TeamID:     event.SandboxTeamID,
		TemplateID: event.SandboxTemplateID,
		BuildID:    buildID,
		TimeoutAt:  nil, // We don't have timeout info in the created event
		Metadata:   nil,
	})
	if err != nil {
		// Check for unique constraint violation (duplicate sandbox_id)
		// This can happen on redelivery - treat as success
		if isDuplicateKeyError(err) {
			logger.L().Debug(ctx, "Sandbox run already exists, skipping",
				logger.WithSandboxID(event.SandboxID))
			return nil
		}
		return err
	}

	return nil
}

func (c *Consumer) handleKilled(ctx context.Context, event events.SandboxEvent) error {
	logger.L().Debug(ctx, "Processing sandbox killed event",
		logger.WithSandboxID(event.SandboxID))

	endReason := "killed"
	if reason, ok := event.EventData["end_reason"].(string); ok && reason != "" {
		endReason = reason
	}

	err := c.db.EndSandboxRun(ctx, queries.EndSandboxRunParams{
		EndReason: &endReason,
		SandboxID: event.SandboxID,
	})

	return err
}

func (c *Consumer) handlePaused(ctx context.Context, event events.SandboxEvent) error {
	logger.L().Debug(ctx, "Processing sandbox paused event",
		logger.WithSandboxID(event.SandboxID))

	err := c.db.UpdateSandboxRunStatus(ctx, queries.UpdateSandboxRunStatusParams{
		Status:    "paused",
		SandboxID: event.SandboxID,
	})

	return err
}

func (c *Consumer) handleResumed(ctx context.Context, event events.SandboxEvent) error {
	logger.L().Debug(ctx, "Processing sandbox resumed event",
		logger.WithSandboxID(event.SandboxID))

	// Resume creates a new sandbox, so we create a new run entry
	// The old sandbox_id stays paused, new one is created
	buildID := &event.SandboxBuildID
	if event.SandboxBuildID == "" {
		buildID = nil
	}

	_, err := c.db.CreateSandboxRun(ctx, queries.CreateSandboxRunParams{
		SandboxID:  event.SandboxID,
		TeamID:     event.SandboxTeamID,
		TemplateID: event.SandboxTemplateID,
		BuildID:    buildID,
		TimeoutAt:  nil,
		Metadata:   nil,
	})
	if err != nil {
		if isDuplicateKeyError(err) {
			// If sandbox already exists, just update status to running
			return c.db.UpdateSandboxRunStatus(ctx, queries.UpdateSandboxRunStatusParams{
				Status:    "running",
				SandboxID: event.SandboxID,
			})
		}
		return err
	}

	return nil
}

func (c *Consumer) claimPendingMessages(ctx context.Context) {
	// Claim messages pending > 5 minutes (from crashed consumers)
	messages, _, _ := c.redis.XAutoClaim(ctx, &redis.XAutoClaimArgs{
		Stream:   events.SandboxEventsStreamName,
		Group:    groupName,
		Consumer: c.consumerID,
		MinIdle:  claimTime,
		Start:    "0",
		Count:    10,
	}).Result()

	for _, msg := range messages {
		if err := c.processMessage(ctx, msg); err == nil {
			c.redis.XAck(ctx, events.SandboxEventsStreamName, groupName, msg.ID)
		}
	}
}

func isDuplicateKeyError(err error) bool {
	if err == nil {
		return false
	}
	// PostgreSQL unique constraint violation
	return contains(err.Error(), "duplicate key") || contains(err.Error(), "unique constraint")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr, 0))
}

func containsAt(s, substr string, start int) bool {
	for i := start; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
