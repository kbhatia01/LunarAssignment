package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"lunar-rockets/src/internal/domain"
	"lunar-rockets/src/internal/service/repository"
)

type rocketStateRepository struct {
	tx queryExecutor
}

func (d *Database) Get(ctx context.Context, channel string) (domain.RocketState, bool, error) {
	return getRocketState(ctx, d.db, channel)
}

func (d *Database) List(ctx context.Context, options repository.ListRocketsOptions) ([]domain.RocketState, error) {
	return listRocketStates(ctx, d.db, options)
}

func (r rocketStateRepository) Upsert(ctx context.Context, state domain.RocketState) error {
	var reason any
	if state.ExplosionReason != nil {
		reason = *state.ExplosionReason
	}
	_, err := r.tx.ExecContext(ctx, `
		INSERT INTO rocket_states (
			channel,
			rocket_type,
			mission,
			speed,
			status,
			explosion_reason,
			last_message_number,
			last_message_time,
			pending_events,
			updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(channel) DO UPDATE SET
			rocket_type = excluded.rocket_type,
			mission = excluded.mission,
			speed = excluded.speed,
			status = excluded.status,
			explosion_reason = excluded.explosion_reason,
			last_message_number = excluded.last_message_number,
			last_message_time = excluded.last_message_time,
			pending_events = excluded.pending_events,
			updated_at = excluded.updated_at
	`, state.Channel, state.Type, state.Mission, state.Speed, state.Status, reason, state.LastMessageNumber, state.LastMessageTime.Format(time.RFC3339Nano), state.PendingEvents, state.UpdatedAt.Format(time.RFC3339Nano))
	return err
}

func (r rocketStateRepository) Delete(ctx context.Context, channel string) error {
	_, err := r.tx.ExecContext(ctx, `DELETE FROM rocket_states WHERE channel = ?`, channel)
	return err
}

func (r rocketStateRepository) Get(ctx context.Context, channel string) (domain.RocketState, bool, error) {
	return getRocketState(ctx, r.tx, channel)
}

func (r rocketStateRepository) List(ctx context.Context, options repository.ListRocketsOptions) ([]domain.RocketState, error) {
	return listRocketStates(ctx, r.tx, options)
}

func getRocketState(ctx context.Context, db queryer, channel string) (domain.RocketState, bool, error) {
	row := db.QueryRowContext(ctx, `
		SELECT
			channel,
			rocket_type,
			mission,
			speed,
			status,
			explosion_reason,
			last_message_number,
			last_message_time,
			pending_events,
			updated_at
		FROM rocket_states
		WHERE channel = ?
	`, channel)
	state, err := scanRocketState(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.RocketState{}, false, nil
	}
	if err != nil {
		return domain.RocketState{}, false, err
	}
	return state, true, nil
}

func listRocketStates(ctx context.Context, db queryer, options repository.ListRocketsOptions) ([]domain.RocketState, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT
			channel,
			rocket_type,
			mission,
			speed,
			status,
			explosion_reason,
			last_message_number,
			last_message_time,
			pending_events,
			updated_at
		FROM rocket_states
		ORDER BY `+orderBy(options))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var states []domain.RocketState
	for rows.Next() {
		state, err := scanRocketState(rows)
		if err != nil {
			return nil, err
		}
		states = append(states, state)
	}
	return states, rows.Err()
}
