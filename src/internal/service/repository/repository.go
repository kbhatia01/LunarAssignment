package repository

import (
	"context"
	"errors"
	"time"

	"lunar-rockets/src/internal/domain"
)

var ErrConflict = errors.New("conflicting duplicate event")

type Clock interface {
	Now() time.Time
}

type HealthChecker interface {
	Ping(ctx context.Context) error
}

type UnitOfWork interface {
	WithinTx(ctx context.Context, fn func(ctx context.Context, repos Repositories) error) error
}

type Repositories interface {
	Events() EventRepository
	RocketStates() RocketStateRepository
}

type InsertOutcome string

const (
	Inserted  InsertOutcome = "inserted"
	Duplicate InsertOutcome = "duplicate"
)

type EventRepository interface {
	Insert(ctx context.Context, event domain.Event, receivedAt time.Time) (InsertOutcome, error)
	ListByChannel(ctx context.Context, channel string) ([]domain.Event, error)
	ListFrom(ctx context.Context, channel string, fromMessageNumber int64) ([]domain.Event, error)
	CountAfter(ctx context.Context, channel string, messageNumber int64) (int64, error)
}

type RocketStateReader interface {
	Get(ctx context.Context, channel string) (domain.RocketState, bool, error)
	List(ctx context.Context, options ListRocketsOptions) ([]domain.RocketState, error)
}

type RocketStateRepository interface {
	RocketStateReader
	Upsert(ctx context.Context, state domain.RocketState) error
	Delete(ctx context.Context, channel string) error
}

type ListRocketsOptions struct {
	Sort  string
	Order string
}

const (
	SortChannel           = "channel"
	SortMission           = "mission"
	SortSpeed             = "speed"
	SortStatus            = "status"
	SortLastMessageNumber = "lastMessageNumber"

	OrderAsc  = "asc"
	OrderDesc = "desc"
)
