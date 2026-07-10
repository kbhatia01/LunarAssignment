package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"lunar-rockets/src/internal/domain"
	"lunar-rockets/src/internal/service/repository"
)

type IngestMessageService struct {
	uow    repository.UnitOfWork
	clock  repository.Clock
	logger *slog.Logger
}

type IngestMessageCommand struct {
	Metadata domain.Metadata
	Message  json.RawMessage
}

type IngestStatus string

const (
	IngestStatusCreated                     IngestStatus = "created"
	IngestStatusDuplicate                   IngestStatus = "duplicate"
	IngestStatusConflictingDuplicateIgnored IngestStatus = "conflict_ignored"
)

type IngestMessageResult struct {
	Status        IngestStatus
	Channel       string
	MessageNumber int64
	Materialized  bool
}

func NewIngestMessageService(uow repository.UnitOfWork, clock repository.Clock, options ...IngestOption) IngestMessageService {
	service := IngestMessageService{
		uow:    uow,
		clock:  clock,
		logger: slog.Default(),
	}
	for _, option := range options {
		option(&service)
	}
	return service
}

func (s IngestMessageService) Handle(ctx context.Context, command IngestMessageCommand) (IngestMessageResult, error) {
	event, err := domain.NewEvent(domain.Envelope{
		Metadata: command.Metadata,
		Message:  command.Message,
	})
	if err != nil {
		return IngestMessageResult{}, err
	}

	result, err := s.ingestEvent(ctx, event)
	if err != nil {
		return IngestMessageResult{}, err
	}

	s.recordOutcome(ctx, result)
	return result, nil
}

func (s IngestMessageService) ingestEvent(ctx context.Context, event domain.Event) (IngestMessageResult, error) {
	result := IngestMessageResult{
		Channel:       event.Channel,
		MessageNumber: event.MessageNumber,
	}
	now := s.clock.Now()

	err := s.uow.WithinTx(ctx, func(ctx context.Context, repos repository.Repositories) error {
		outcome, err := repos.Events().Insert(ctx, event, now)
		if err != nil {
			if err == repository.ErrConflict {
				result.Status = IngestStatusConflictingDuplicateIgnored
				result.Materialized = false
				return nil
			}
			return err
		}
		if outcome == repository.Duplicate {
			result.Status = IngestStatusDuplicate
			result.Materialized = false
			return nil
		}

		result.Status = IngestStatusCreated
		materialized, err := s.materialize(ctx, repos, event, now)
		result.Materialized = materialized
		return err
	})
	if err != nil {
		return IngestMessageResult{}, err
	}

	return result, nil
}

func (s IngestMessageService) materialize(ctx context.Context, repos repository.Repositories, event domain.Event, now time.Time) (bool, error) {
	currentState, fromMessageNumber, err := s.currentReplayPosition(ctx, repos, event.Channel)
	if err != nil {
		return false, err
	}

	events, err := repos.Events().ListFrom(ctx, event.Channel, fromMessageNumber)
	if err != nil {
		return false, err
	}
	applied, err := domain.ApplyContiguous(event.Channel, currentState, events, now)
	if err != nil {
		s.recordReplayFailure(ctx, event, err)
		return false, err
	}
	if applied.State == nil || applied.Invalid {
		if err := repos.RocketStates().Delete(ctx, event.Channel); err != nil {
			return false, err
		}
		return false, nil
	}

	pendingEvents, err := repos.Events().CountAfter(ctx, event.Channel, applied.State.LastMessageNumber)
	if err != nil {
		return false, err
	}
	applied.State.PendingEvents = pendingEvents
	if err := repos.RocketStates().Upsert(ctx, *applied.State); err != nil {
		return false, err
	}
	return true, nil
}

func (s IngestMessageService) currentReplayPosition(ctx context.Context, repos repository.Repositories, channel string) (*domain.RocketState, int64, error) {
	current, found, err := repos.RocketStates().Get(ctx, channel)
	if err != nil {
		return nil, 0, err
	}
	if found && current.LastMessageNumber > 0 {
		return &current, current.LastMessageNumber + 1, nil
	}
	return nil, 1, nil
}

func (s IngestMessageService) recordReplayFailure(ctx context.Context, event domain.Event, err error) {
	s.logger.ErrorContext(ctx, "rocket replay failed",
		slog.String("channel", event.Channel),
		slog.Int64("message_number", event.MessageNumber),
		slog.String("message_type", event.MessageType),
		slog.Any("error", err),
	)
}

func (s IngestMessageService) recordOutcome(ctx context.Context, result IngestMessageResult) {
	s.logger.InfoContext(ctx, "message ingested",
		slog.String("status", string(result.Status)),
		slog.String("channel", result.Channel),
		slog.Int64("message_number", result.MessageNumber),
		slog.Bool("materialized", result.Materialized),
	)
}
