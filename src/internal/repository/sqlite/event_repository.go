package sqlite

import (
	"context"
	"errors"
	"time"

	"lunar-rockets/src/internal/domain"
	"lunar-rockets/src/internal/service/repository"

	moderncsqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

type eventRepository struct {
	tx queryExecutor
}

func (r eventRepository) Insert(ctx context.Context, event domain.Event, receivedAt time.Time) (repository.InsertOutcome, error) {
	_, err := r.tx.ExecContext(ctx, `
		INSERT INTO events (
			channel,
			message_number,
			message_time,
			message_type,
			payload_json,
			event_hash,
			received_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)
	`, event.Channel, event.MessageNumber, event.MessageTime.Format(time.RFC3339Nano), event.MessageType, event.PayloadJSON, event.EventHash, receivedAt.Format(time.RFC3339Nano))
	if err == nil {
		return repository.Inserted, nil
	}
	if !isConstraintError(err) {
		return "", err
	}

	var existingHash string
	row := r.tx.QueryRowContext(ctx, `
		SELECT event_hash
		FROM events
		WHERE channel = ? AND message_number = ?
	`, event.Channel, event.MessageNumber)
	if err := row.Scan(&existingHash); err != nil {
		return "", err
	}
	if existingHash != event.EventHash {
		return "", repository.ErrConflict
	}
	return repository.Duplicate, nil
}

func (r eventRepository) ListByChannel(ctx context.Context, channel string) ([]domain.Event, error) {
	return r.ListFrom(ctx, channel, 1)
}

func (r eventRepository) ListFrom(ctx context.Context, channel string, fromMessageNumber int64) ([]domain.Event, error) {
	rows, err := r.tx.QueryContext(ctx, `
		SELECT channel, message_number, message_time, message_type, payload_json, event_hash
		FROM events
		WHERE channel = ? AND message_number >= ?
		ORDER BY message_number
	`, channel, fromMessageNumber)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []domain.Event
	for rows.Next() {
		event, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (r eventRepository) CountAfter(ctx context.Context, channel string, messageNumber int64) (int64, error) {
	var count int64
	row := r.tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM events
		WHERE channel = ? AND message_number > ?
	`, channel, messageNumber)
	if err := row.Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func isConstraintError(err error) bool {
	var sqliteErr *moderncsqlite.Error
	if !errors.As(err, &sqliteErr) {
		return false
	}
	switch sqliteErr.Code() {
	case int(sqlite3.SQLITE_CONSTRAINT),
		int(sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY),
		int(sqlite3.SQLITE_CONSTRAINT_UNIQUE):
		return true
	default:
		return false
	}
}
