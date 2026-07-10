package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"lunar-rockets/src/internal/domain"
	"lunar-rockets/src/internal/service/repository"
)

func TestDatabaseMigrationsAndHealth(t *testing.T) {
	db := openTestDB(t)
	if err := db.Ping(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
}

func TestEventDuplicateAndConflictDetection(t *testing.T) {
	db := openTestDB(t)
	event := launchEvent("rocket-1", 1, "ARTEMIS")

	if err := db.WithinTx(context.Background(), func(ctx context.Context, repos repository.Repositories) error {
		outcome, err := repos.Events().Insert(ctx, event, time.Now())
		if err != nil {
			return err
		}
		if outcome != repository.Inserted {
			t.Fatalf("expected inserted, got %s", outcome)
		}
		return nil
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	if err := db.WithinTx(context.Background(), func(ctx context.Context, repos repository.Repositories) error {
		outcome, err := repos.Events().Insert(ctx, event, time.Now())
		if err != nil {
			return err
		}
		if outcome != repository.Duplicate {
			t.Fatalf("expected duplicate, got %s", outcome)
		}
		return nil
	}); err != nil {
		t.Fatalf("duplicate insert: %v", err)
	}

	conflict := launchEvent("rocket-1", 1, "GEMINI")
	err := db.WithinTx(context.Background(), func(ctx context.Context, repos repository.Repositories) error {
		_, err := repos.Events().Insert(ctx, conflict, time.Now())
		return err
	})
	if !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
}

func TestListByChannelOrdersByMessageNumber(t *testing.T) {
	db := openTestDB(t)
	events := []domain.Event{
		speedEvent("rocket-1", 3, domain.MessageTypeRocketSpeedIncreased, 100),
		launchEvent("rocket-1", 1, "ARTEMIS"),
		speedEvent("rocket-1", 2, domain.MessageTypeRocketSpeedIncreased, 50),
	}

	if err := db.WithinTx(context.Background(), func(ctx context.Context, repos repository.Repositories) error {
		for _, event := range events {
			if _, err := repos.Events().Insert(ctx, event, time.Now()); err != nil {
				return err
			}
		}
		ordered, err := repos.Events().ListByChannel(ctx, "rocket-1")
		if err != nil {
			return err
		}
		for i, event := range ordered {
			expected := int64(i + 1)
			if event.MessageNumber != expected {
				t.Fatalf("expected message %d, got %d", expected, event.MessageNumber)
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("transaction: %v", err)
	}
}

func TestRocketStateSortingTieBreaker(t *testing.T) {
	db := openTestDB(t)
	now := time.Now().UTC()

	if err := db.WithinTx(context.Background(), func(ctx context.Context, repos repository.Repositories) error {
		for _, channel := range []string{"rocket-b", "rocket-a"} {
			if err := repos.RocketStates().Upsert(ctx, domain.RocketState{
				Channel:           channel,
				Type:              "Falcon-9",
				Mission:           "ARTEMIS",
				Speed:             500,
				Status:            domain.StatusLaunched,
				LastMessageNumber: 1,
				LastMessageTime:   now,
				UpdatedAt:         now,
			}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("upsert states: %v", err)
	}

	states, err := db.List(context.Background(), repository.ListRocketsOptions{
		Sort:  repository.SortMission,
		Order: repository.OrderAsc,
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(states) != 2 {
		t.Fatalf("expected two states, got %d", len(states))
	}
	if states[0].Channel != "rocket-a" || states[1].Channel != "rocket-b" {
		t.Fatalf("expected channel tie-breaker, got %#v", states)
	}
}

func openTestDB(t *testing.T) *Database {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "rockets.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	return db
}

func launchEvent(channel string, number int64, mission string) domain.Event {
	return newEvent(channel, number, domain.MessageTypeRocketLaunched, `{"type":"Falcon-9","launchSpeed":500,"mission":"`+mission+`"}`)
}

func speedEvent(channel string, number int64, messageType string, by int64) domain.Event {
	return newEvent(channel, number, messageType, `{"by":`+itoa(by)+`}`)
}

func newEvent(channel string, number int64, messageType string, payload string) domain.Event {
	event := domain.Event{
		Channel:       channel,
		MessageNumber: number,
		MessageTime:   time.Date(2022, 2, 2, 19, int(number), 0, 0, time.UTC),
		MessageType:   messageType,
		PayloadJSON:   payload,
	}
	event.EventHash, _ = domain.HashEvent(event)
	return event
}

func itoa(value int64) string {
	if value == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for value > 0 {
		i--
		buf[i] = byte('0' + value%10)
		value /= 10
	}
	return string(buf[i:])
}
