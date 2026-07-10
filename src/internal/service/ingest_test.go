package service

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"testing"
	"time"

	"lunar-rockets/src/internal/domain"
	"lunar-rockets/src/internal/service/repository"
)

func TestIngestNewEventMaterializesState(t *testing.T) {
	fake := newFakeStore()
	service := NewIngestMessageService(fake, fixedClock{})

	result, err := service.Handle(context.Background(), launchCommand(1))
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if result.Status != IngestStatusCreated || !result.Materialized {
		t.Fatalf("unexpected result: %#v", result)
	}
	if fake.state == nil || fake.state.Channel != "rocket-1" {
		t.Fatalf("expected upserted state, got %#v", fake.state)
	}
}

func TestIngestExactDuplicateDoesNotReplay(t *testing.T) {
	fake := newFakeStore()
	service := NewIngestMessageService(fake, fixedClock{})
	if _, err := service.Handle(context.Background(), launchCommand(1)); err != nil {
		t.Fatalf("initial handle: %v", err)
	}
	fake.replayReads = 0

	result, err := service.Handle(context.Background(), launchCommand(1))
	if err != nil {
		t.Fatalf("duplicate handle: %v", err)
	}
	if result.Status != IngestStatusDuplicate || result.Materialized {
		t.Fatalf("unexpected duplicate result: %#v", result)
	}
	if fake.replayReads != 0 {
		t.Fatalf("duplicate should not read events for replay, got %d reads", fake.replayReads)
	}
}

func TestIngestConflictingDuplicateIsIgnored(t *testing.T) {
	fake := newFakeStore()
	service := NewIngestMessageService(fake, fixedClock{})
	if _, err := service.Handle(context.Background(), launchCommand(1)); err != nil {
		t.Fatalf("initial handle: %v", err)
	}

	conflict := launchCommand(1)
	conflict.Message = json.RawMessage(`{"type":"Falcon-9","launchSpeed":500,"mission":"GEMINI"}`)
	result, err := service.Handle(context.Background(), conflict)
	if err != nil {
		t.Fatalf("conflicting duplicate should be ignored, got error %v", err)
	}
	if result.Status != IngestStatusConflictingDuplicateIgnored || result.Materialized {
		t.Fatalf("unexpected conflict ignored result: %#v", result)
	}
}

func TestIngestUsesIncrementalReplayFromLastAppliedMessage(t *testing.T) {
	fake := newFakeStore()
	service := NewIngestMessageService(fake, fixedClock{})
	if _, err := service.Handle(context.Background(), launchCommand(1)); err != nil {
		t.Fatalf("initial handle: %v", err)
	}
	if _, err := service.Handle(context.Background(), speedCommand(3, 100)); err != nil {
		t.Fatalf("gap handle: %v", err)
	}
	if fake.state == nil || fake.state.LastMessageNumber != 1 || fake.state.PendingEvents != 1 {
		t.Fatalf("expected message 3 to remain pending, got %#v", fake.state)
	}

	fake.lastListFrom = 0
	if _, err := service.Handle(context.Background(), speedCommand(2, 50)); err != nil {
		t.Fatalf("gap fill handle: %v", err)
	}
	if fake.lastListFrom != 2 {
		t.Fatalf("expected incremental replay from message 2, got %d", fake.lastListFrom)
	}
	if fake.state == nil || fake.state.LastMessageNumber != 3 || fake.state.PendingEvents != 0 || fake.state.Speed != 650 {
		t.Fatalf("expected messages 2 and 3 applied incrementally, got %#v", fake.state)
	}
}

func TestIngestInvalidReplayDeletesExistingState(t *testing.T) {
	fake := newFakeStore()
	fake.state = &domain.RocketState{Channel: "rocket-1"}
	service := NewIngestMessageService(fake, fixedClock{})

	command := IngestMessageCommand{
		Metadata: domain.Metadata{
			Channel:       "rocket-1",
			MessageNumber: 1,
			MessageTime:   "2022-02-02T19:39:05Z",
			MessageType:   domain.MessageTypeRocketSpeedIncreased,
		},
		Message: json.RawMessage(`{"by":100}`),
	}
	result, err := service.Handle(context.Background(), command)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if result.Materialized {
		t.Fatalf("expected non-materialized result: %#v", result)
	}
	if fake.state != nil {
		t.Fatalf("expected deleted state, got %#v", fake.state)
	}
}

func TestListSortValidation(t *testing.T) {
	service := NewRocketQueryService(newFakeStore())

	if _, err := service.List(context.Background(), ListRocketsQuery{Sort: "bad"}); !errors.Is(err, ErrInvalidSort) {
		t.Fatalf("expected invalid sort, got %v", err)
	}
	if _, err := service.List(context.Background(), ListRocketsQuery{Order: "sideways"}); !errors.Is(err, ErrInvalidSort) {
		t.Fatalf("expected invalid order, got %v", err)
	}
}

type fixedClock struct{}

func (fixedClock) Now() time.Time {
	return time.Date(2026, 7, 9, 22, 51, 39, 0, time.UTC)
}

type fakeStore struct {
	events       []domain.Event
	state        *domain.RocketState
	replayReads  int
	lastListFrom int64
}

func newFakeStore() *fakeStore {
	return &fakeStore{}
}

func (f *fakeStore) WithinTx(ctx context.Context, fn func(context.Context, repository.Repositories) error) error {
	return fn(ctx, f)
}

func (f *fakeStore) Events() repository.EventRepository {
	return f
}

func (f *fakeStore) RocketStates() repository.RocketStateRepository {
	return f
}

func (f *fakeStore) Insert(ctx context.Context, event domain.Event, receivedAt time.Time) (repository.InsertOutcome, error) {
	for _, existing := range f.events {
		if existing.Channel == event.Channel && existing.MessageNumber == event.MessageNumber {
			if existing.EventHash != event.EventHash {
				return "", repository.ErrConflict
			}
			return repository.Duplicate, nil
		}
	}
	f.events = append(f.events, event)
	return repository.Inserted, nil
}

func (f *fakeStore) ListByChannel(ctx context.Context, channel string) ([]domain.Event, error) {
	f.replayReads++
	return f.ListFrom(ctx, channel, 1)
}

func (f *fakeStore) ListFrom(ctx context.Context, channel string, fromMessageNumber int64) ([]domain.Event, error) {
	f.replayReads++
	f.lastListFrom = fromMessageNumber
	var events []domain.Event
	for _, event := range f.events {
		if event.Channel == channel && event.MessageNumber >= fromMessageNumber {
			events = append(events, event)
		}
	}
	sort.Slice(events, func(i, j int) bool {
		return events[i].MessageNumber < events[j].MessageNumber
	})
	return events, nil
}

func (f *fakeStore) CountAfter(ctx context.Context, channel string, messageNumber int64) (int64, error) {
	var count int64
	for _, event := range f.events {
		if event.Channel == channel && event.MessageNumber > messageNumber {
			count++
		}
	}
	return count, nil
}

func (f *fakeStore) Upsert(ctx context.Context, state domain.RocketState) error {
	f.state = &state
	return nil
}

func (f *fakeStore) Delete(ctx context.Context, channel string) error {
	f.state = nil
	return nil
}

func (f *fakeStore) Get(ctx context.Context, channel string) (domain.RocketState, bool, error) {
	if f.state == nil || f.state.Channel != channel {
		return domain.RocketState{}, false, nil
	}
	return *f.state, true, nil
}

func (f *fakeStore) List(ctx context.Context, options repository.ListRocketsOptions) ([]domain.RocketState, error) {
	if f.state == nil {
		return nil, nil
	}
	return []domain.RocketState{*f.state}, nil
}

func launchCommand(number int64) IngestMessageCommand {
	return IngestMessageCommand{
		Metadata: domain.Metadata{
			Channel:       "rocket-1",
			MessageNumber: number,
			MessageTime:   "2022-02-02T19:39:05Z",
			MessageType:   domain.MessageTypeRocketLaunched,
		},
		Message: json.RawMessage(`{"type":"Falcon-9","launchSpeed":500,"mission":"ARTEMIS"}`),
	}
}

func speedCommand(number int64, by int64) IngestMessageCommand {
	return IngestMessageCommand{
		Metadata: domain.Metadata{
			Channel:       "rocket-1",
			MessageNumber: number,
			MessageTime:   "2022-02-02T19:39:05Z",
			MessageType:   domain.MessageTypeRocketSpeedIncreased,
		},
		Message: json.RawMessage(`{"by":` + strconv.FormatInt(by, 10) + `}`),
	}
}
