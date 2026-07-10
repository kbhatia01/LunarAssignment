package domain

import (
	"encoding/json"
	"time"
)

const (
	StatusLaunched = "launched"
	StatusExploded = "exploded"
)

type RocketState struct {
	Channel           string
	Type              string
	Mission           string
	Speed             int64
	Status            string
	ExplosionReason   *string
	LastMessageNumber int64
	LastMessageTime   time.Time
	PendingEvents     int64
	UpdatedAt         time.Time
}

type ReplayResult struct {
	State   *RocketState
	Invalid bool
}

func Replay(channel string, events []Event, now time.Time) (ReplayResult, error) {
	result, err := ApplyContiguous(channel, nil, events, now)
	if err != nil {
		return ReplayResult{}, err
	}
	if result.State != nil {
		result.State.PendingEvents = int64(len(events) - result.AppliedEvents)
	}
	return ReplayResult{State: result.State, Invalid: result.Invalid}, nil
}

type ApplyResult struct {
	State         *RocketState
	Invalid       bool
	AppliedEvents int
}

func ApplyContiguous(channel string, current *RocketState, events []Event, now time.Time) (ApplyResult, error) {
	if current == nil {
		if len(events) == 0 || events[0].MessageNumber != 1 {
			return ApplyResult{}, nil
		}
		if events[0].MessageType != MessageTypeRocketLaunched {
			return ApplyResult{Invalid: true}, nil
		}
	}

	var state *RocketState
	expected := int64(1)
	if current != nil {
		copy := *current
		state = &copy
		expected = current.LastMessageNumber + 1
	}

	for i, event := range events {
		if event.MessageNumber != expected {
			break
		}
		expected++

		switch event.MessageType {
		case MessageTypeRocketLaunched:
			if event.MessageNumber != 1 {
				state.LastMessageNumber = event.MessageNumber
				state.LastMessageTime = event.MessageTime
				continue
			}
			var payload RocketLaunched
			if err := json.Unmarshal([]byte(event.PayloadJSON), &payload); err != nil {
				return ApplyResult{}, err
			}
			state = &RocketState{
				Channel:           channel,
				Type:              payload.Type,
				Mission:           payload.Mission,
				Speed:             payload.LaunchSpeed,
				Status:            StatusLaunched,
				LastMessageNumber: event.MessageNumber,
				LastMessageTime:   event.MessageTime,
				UpdatedAt:         now,
			}
		case MessageTypeRocketSpeedIncreased:
			if state == nil {
				continue
			}
			var payload RocketSpeedChanged
			if err := json.Unmarshal([]byte(event.PayloadJSON), &payload); err != nil {
				return ApplyResult{}, err
			}
			state.Speed += payload.By
			state.LastMessageNumber = event.MessageNumber
			state.LastMessageTime = event.MessageTime
		case MessageTypeRocketSpeedDecreased:
			if state == nil {
				continue
			}
			var payload RocketSpeedChanged
			if err := json.Unmarshal([]byte(event.PayloadJSON), &payload); err != nil {
				return ApplyResult{}, err
			}
			state.Speed -= payload.By
			state.LastMessageNumber = event.MessageNumber
			state.LastMessageTime = event.MessageTime
		case MessageTypeRocketMissionChanged:
			if state == nil {
				continue
			}
			var payload RocketMissionChanged
			if err := json.Unmarshal([]byte(event.PayloadJSON), &payload); err != nil {
				return ApplyResult{}, err
			}
			state.Mission = payload.NewMission
			state.LastMessageNumber = event.MessageNumber
			state.LastMessageTime = event.MessageTime
		case MessageTypeRocketExploded:
			if state == nil {
				continue
			}
			var payload RocketExploded
			if err := json.Unmarshal([]byte(event.PayloadJSON), &payload); err != nil {
				return ApplyResult{}, err
			}
			state.Status = StatusExploded
			state.ExplosionReason = &payload.Reason
			state.LastMessageNumber = event.MessageNumber
			state.LastMessageTime = event.MessageTime
		default:
			return ApplyResult{Invalid: true}, nil
		}
		if state != nil {
			state.UpdatedAt = now
		}
		if i == len(events)-1 || events[i+1].MessageNumber != expected {
			if state == nil {
				return ApplyResult{Invalid: true}, nil
			}
			return ApplyResult{State: state, AppliedEvents: i + 1}, nil
		}
	}

	return ApplyResult{State: state}, nil
}
