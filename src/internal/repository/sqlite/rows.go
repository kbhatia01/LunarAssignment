package sqlite

import (
	"context"
	"database/sql"
	"time"

	"lunar-rockets/src/internal/domain"
)

type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type queryExecutor interface {
	queryer
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

type scanner interface {
	Scan(dest ...any) error
}

func scanEvent(scanner scanner) (domain.Event, error) {
	var event domain.Event
	var messageTime string
	err := scanner.Scan(
		&event.Channel,
		&event.MessageNumber,
		&messageTime,
		&event.MessageType,
		&event.PayloadJSON,
		&event.EventHash,
	)
	if err != nil {
		return domain.Event{}, err
	}
	event.MessageTime, err = time.Parse(time.RFC3339Nano, messageTime)
	if err != nil {
		return domain.Event{}, err
	}
	return event, nil
}

func scanRocketState(scanner scanner) (domain.RocketState, error) {
	var state domain.RocketState
	var reason sql.NullString
	var lastMessageTime string
	var updatedAt string
	err := scanner.Scan(
		&state.Channel,
		&state.Type,
		&state.Mission,
		&state.Speed,
		&state.Status,
		&reason,
		&state.LastMessageNumber,
		&lastMessageTime,
		&state.PendingEvents,
		&updatedAt,
	)
	if err != nil {
		return domain.RocketState{}, err
	}
	if reason.Valid {
		state.ExplosionReason = &reason.String
	}
	state.LastMessageTime, err = time.Parse(time.RFC3339Nano, lastMessageTime)
	if err != nil {
		return domain.RocketState{}, err
	}
	state.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return domain.RocketState{}, err
	}
	return state, nil
}
