package sqlite

import (
	"context"
	"database/sql"

	"lunar-rockets/src/internal/service/repository"
)

func (d *Database) WithinTx(ctx context.Context, fn func(context.Context, repository.Repositories) error) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	repos := txRepositories{tx: tx}
	if err := fn(ctx, repos); err != nil {
		return err
	}
	return tx.Commit()
}

type txRepositories struct {
	tx *sql.Tx
}

func (r txRepositories) Events() repository.EventRepository {
	return eventRepository{tx: r.tx}
}

func (r txRepositories) RocketStates() repository.RocketStateRepository {
	return rocketStateRepository{tx: r.tx}
}
