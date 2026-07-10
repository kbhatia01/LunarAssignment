package sqlite

import (
	"context"
	"database/sql"

	_ "modernc.org/sqlite"
)

type Database struct {
	db *sql.DB
}

func Open(path string) (*Database, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)

	database := &Database{db: db}
	if err := database.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return database, nil
}

func (d *Database) Close() error {
	return d.db.Close()
}

func (d *Database) Ping(ctx context.Context) error {
	var one int
	return d.db.QueryRowContext(ctx, "SELECT 1").Scan(&one)
}

func (d *Database) migrate(ctx context.Context) error {
	statements := []string{
		`PRAGMA journal_mode = WAL`,
		`PRAGMA busy_timeout = 5000`,
		`CREATE TABLE IF NOT EXISTS events (
			channel TEXT NOT NULL,
			message_number INTEGER NOT NULL,
			message_time TEXT NOT NULL,
			message_type TEXT NOT NULL,
			payload_json TEXT NOT NULL,
			event_hash TEXT NOT NULL,
			received_at TEXT NOT NULL,
			PRIMARY KEY (channel, message_number)
		)`,
		`CREATE TABLE IF NOT EXISTS rocket_states (
			channel TEXT PRIMARY KEY,
			rocket_type TEXT NOT NULL,
			mission TEXT NOT NULL,
			speed INTEGER NOT NULL,
			status TEXT NOT NULL,
			explosion_reason TEXT NULL,
			last_message_number INTEGER NOT NULL,
			last_message_time TEXT NOT NULL,
			pending_events INTEGER NOT NULL,
			updated_at TEXT NOT NULL
		)`,
	}
	for _, statement := range statements {
		if _, err := d.db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}
