package events

import (
	"context"
	"errors"
	"sync"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/moru-ai/sandbox-infra/packages/shared/pkg/events"
	"github.com/moru-ai/sandbox-infra/packages/shared/pkg/logger"
)

type VolumeEventsService struct {
	deliveryTargets []events.Delivery[events.VolumeEvent]
}

func NewVolumeEventsService(deliveryTargets []events.Delivery[events.VolumeEvent]) *VolumeEventsService {
	return &VolumeEventsService{
		deliveryTargets: deliveryTargets,
	}
}

func (e *VolumeEventsService) Publish(ctx context.Context, teamID uuid.UUID, event events.VolumeEvent) {
	deliveryKey := events.DeliveryKey(teamID)

	err := validateVolumeEvent(event)
	if err != nil {
		logger.L().Error(ctx, "Failed to publish volume event due to validation error", zap.Error(err), zap.Any("event", event))

		return
	}

	wg := sync.WaitGroup{}
	for _, target := range e.deliveryTargets {
		wg.Go(func() {
			if err := target.Publish(ctx, deliveryKey, event); err != nil {
				logger.L().Error(ctx, "Failed to publish volume event", zap.Error(err), zap.Any("event", event))
			}
		})
	}
	wg.Wait()
}

func (e *VolumeEventsService) Close(ctx context.Context) error {
	var err error
	for _, target := range e.deliveryTargets {
		closeErr := target.Close(ctx)
		err = errors.Join(err, closeErr)
	}

	return err
}

func validateVolumeEvent(event events.VolumeEvent) error {
	if event.Version == "" {
		return &EventFieldMissingError{"version"}
	}

	if event.Type == "" {
		return &EventFieldMissingError{"type"}
	}

	if event.VolumeID == "" {
		return &EventFieldMissingError{"volume_id"}
	}

	if event.Timestamp.IsZero() {
		return &EventFieldMissingError{"timestamp"}
	}

	return nil
}
