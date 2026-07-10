package domain

import (
	"testing"
	"time"
)

func TestReplayAppliesOrderedEventsAndAllowsNegativeSpeed(t *testing.T) {
	events := []Event{
		event(1, MessageTypeRocketLaunched, `{"type":"Falcon-9","launchSpeed":500,"mission":"ARTEMIS"}`),
		event(2, MessageTypeRocketSpeedIncreased, `{"by":100}`),
		event(3, MessageTypeRocketSpeedDecreased, `{"by":1000}`),
		event(4, MessageTypeRocketMissionChanged, `{"newMission":"SHUTTLE_MIR"}`),
	}

	result, err := Replay("rocket-1", events, time.Now())
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if result.State == nil {
		t.Fatal("expected materialized state")
	}
	if result.State.Speed != -400 {
		t.Fatalf("expected negative speed, got %d", result.State.Speed)
	}
	if result.State.Mission != "SHUTTLE_MIR" {
		t.Fatalf("expected mission change, got %q", result.State.Mission)
	}
}

func TestReplayStopsAtSequenceGapAndCountsPendingEvents(t *testing.T) {
	events := []Event{
		event(1, MessageTypeRocketLaunched, `{"type":"Falcon-9","launchSpeed":500,"mission":"ARTEMIS"}`),
		event(2, MessageTypeRocketSpeedIncreased, `{"by":100}`),
		event(4, MessageTypeRocketSpeedIncreased, `{"by":100}`),
		event(5, MessageTypeRocketSpeedIncreased, `{"by":100}`),
	}

	result, err := Replay("rocket-1", events, time.Now())
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if result.State.LastMessageNumber != 2 {
		t.Fatalf("expected last applied message 2, got %d", result.State.LastMessageNumber)
	}
	if result.State.PendingEvents != 2 {
		t.Fatalf("expected 2 pending events, got %d", result.State.PendingEvents)
	}
}

func TestReplayExplosionDoesNotStopLaterEvents(t *testing.T) {
	events := []Event{
		event(1, MessageTypeRocketLaunched, `{"type":"Falcon-9","launchSpeed":500,"mission":"ARTEMIS"}`),
		event(2, MessageTypeRocketExploded, `{"reason":"PRESSURE_VESSEL_FAILURE"}`),
		event(3, MessageTypeRocketSpeedIncreased, `{"by":100}`),
		event(4, MessageTypeRocketMissionChanged, `{"newMission":"APOLLO"}`),
	}

	result, err := Replay("rocket-1", events, time.Now())
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if result.State.Status != StatusExploded {
		t.Fatalf("expected exploded status, got %q", result.State.Status)
	}
	if result.State.ExplosionReason == nil || *result.State.ExplosionReason != "PRESSURE_VESSEL_FAILURE" {
		t.Fatalf("expected explosion reason, got %#v", result.State.ExplosionReason)
	}
	if result.State.Speed != 600 {
		t.Fatalf("expected later speed event to apply, got %d", result.State.Speed)
	}
	if result.State.Mission != "APOLLO" {
		t.Fatalf("expected later mission event to apply, got %q", result.State.Mission)
	}
}

func TestReplayLaterLaunchDoesNotResetState(t *testing.T) {
	events := []Event{
		event(1, MessageTypeRocketLaunched, `{"type":"Falcon-9","launchSpeed":500,"mission":"ARTEMIS"}`),
		event(2, MessageTypeRocketSpeedIncreased, `{"by":100}`),
		event(3, MessageTypeRocketLaunched, `{"type":"Atlas","launchSpeed":1,"mission":"RESET"}`),
		event(4, MessageTypeRocketSpeedIncreased, `{"by":100}`),
	}

	result, err := Replay("rocket-1", events, time.Now())
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if result.State.Type != "Falcon-9" || result.State.Mission != "ARTEMIS" || result.State.Speed != 700 {
		t.Fatalf("later launch reset state unexpectedly: %#v", result.State)
	}
}

func TestReplayRequiresMessageOneLaunch(t *testing.T) {
	tests := map[string][]Event{
		"missing message one": {
			event(2, MessageTypeRocketSpeedIncreased, `{"by":100}`),
		},
		"message one is not launch": {
			event(1, MessageTypeRocketSpeedIncreased, `{"by":100}`),
		},
	}

	for name, events := range tests {
		t.Run(name, func(t *testing.T) {
			result, err := Replay("rocket-1", events, time.Now())
			if err != nil {
				t.Fatalf("replay: %v", err)
			}
			if result.State != nil {
				t.Fatalf("expected no state, got %#v", result.State)
			}
		})
	}
}

func event(number int64, messageType string, payload string) Event {
	messageTime := time.Date(2022, 2, 2, 19, int(number), 0, 0, time.UTC)
	event := Event{
		Channel:       "rocket-1",
		MessageNumber: number,
		MessageTime:   messageTime,
		MessageType:   messageType,
		PayloadJSON:   payload,
	}
	event.EventHash, _ = HashEvent(event)
	return event
}
