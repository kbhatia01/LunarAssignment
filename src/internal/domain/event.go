package domain

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

const (
	MessageTypeRocketLaunched       = "RocketLaunched"
	MessageTypeRocketSpeedIncreased = "RocketSpeedIncreased"
	MessageTypeRocketSpeedDecreased = "RocketSpeedDecreased"
	MessageTypeRocketExploded       = "RocketExploded"
	MessageTypeRocketMissionChanged = "RocketMissionChanged"
)

type Metadata struct {
	Channel       string `json:"channel"`
	MessageNumber int64  `json:"messageNumber"`
	MessageTime   string `json:"messageTime"`
	MessageType   string `json:"messageType"`
}

type Envelope struct {
	Metadata Metadata        `json:"metadata"`
	Message  json.RawMessage `json:"message"`
}

type Event struct {
	Channel       string
	MessageNumber int64
	MessageTime   time.Time
	MessageType   string
	PayloadJSON   string
	EventHash     string
	Payload       Payload
}

type Payload interface {
	payloadType()
}

type RocketLaunched struct {
	Type        string `json:"type"`
	LaunchSpeed int64  `json:"launchSpeed"`
	Mission     string `json:"mission"`
}

func (RocketLaunched) payloadType() {}

type RocketSpeedChanged struct {
	By int64 `json:"by"`
}

func (RocketSpeedChanged) payloadType() {}

type RocketExploded struct {
	Reason string `json:"reason"`
}

func (RocketExploded) payloadType() {}

type RocketMissionChanged struct {
	NewMission string `json:"newMission"`
}

func (RocketMissionChanged) payloadType() {}

func ParseEnvelope(r io.Reader) (Envelope, error) {
	var envelope Envelope
	decoder := json.NewDecoder(r)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return Envelope{}, validation("invalid JSON: %v", err)
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return Envelope{}, validation("request body must contain one JSON object")
	}
	return envelope, nil
}

func NewEvent(envelope Envelope) (Event, error) {
	metadata := envelope.Metadata
	if metadata.Channel == "" {
		return Event{}, validation("metadata.channel is required")
	}
	if metadata.MessageNumber <= 0 {
		return Event{}, validation("metadata.messageNumber must be positive")
	}
	if metadata.MessageTime == "" {
		return Event{}, validation("metadata.messageTime is required")
	}
	messageTime, err := time.Parse(time.RFC3339Nano, metadata.MessageTime)
	if err != nil {
		return Event{}, validation("metadata.messageTime must be RFC3339/RFC3339Nano: %v", err)
	}
	if metadata.MessageType == "" {
		return Event{}, validation("metadata.messageType is required")
	}
	if len(envelope.Message) == 0 || bytes.Equal(envelope.Message, []byte("null")) {
		return Event{}, validation("message is required")
	}

	payload, payloadJSON, err := parsePayload(metadata.MessageType, envelope.Message)
	if err != nil {
		return Event{}, err
	}

	event := Event{
		Channel:       metadata.Channel,
		MessageNumber: metadata.MessageNumber,
		MessageTime:   messageTime,
		MessageType:   metadata.MessageType,
		PayloadJSON:   payloadJSON,
		Payload:       payload,
	}
	event.EventHash, err = HashEvent(event)
	if err != nil {
		return Event{}, err
	}
	return event, nil
}

func HashEvent(event Event) (string, error) {
	canonical := struct {
		Channel       string          `json:"channel"`
		MessageNumber int64           `json:"messageNumber"`
		MessageTime   string          `json:"messageTime"`
		MessageType   string          `json:"messageType"`
		Message       json.RawMessage `json:"message"`
	}{
		Channel:       event.Channel,
		MessageNumber: event.MessageNumber,
		MessageTime:   event.MessageTime.Format(time.RFC3339Nano),
		MessageType:   event.MessageType,
		Message:       json.RawMessage(event.PayloadJSON),
	}
	bytes, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(bytes)
	return hex.EncodeToString(sum[:]), nil
}

func parsePayload(messageType string, raw json.RawMessage) (Payload, string, error) {
	switch messageType {
	case MessageTypeRocketLaunched:
		var payload RocketLaunched
		if err := decodeStrict(raw, &payload); err != nil {
			return nil, "", validation("invalid RocketLaunched payload: %v", err)
		}
		if payload.Type == "" {
			return nil, "", validation("message.type is required")
		}
		if payload.LaunchSpeed < 0 {
			return nil, "", validation("message.launchSpeed must be non-negative")
		}
		if payload.Mission == "" {
			return nil, "", validation("message.mission is required")
		}
		payloadJSON, err := canonicalJSON(payload)
		if err != nil {
			return nil, "", err
		}
		return payload, payloadJSON, nil
	case MessageTypeRocketSpeedIncreased, MessageTypeRocketSpeedDecreased:
		var payload RocketSpeedChanged
		if err := decodeStrict(raw, &payload); err != nil {
			return nil, "", validation("invalid %s payload: %v", messageType, err)
		}
		if payload.By <= 0 {
			return nil, "", validation("message.by must be positive")
		}
		payloadJSON, err := canonicalJSON(payload)
		if err != nil {
			return nil, "", err
		}
		return payload, payloadJSON, nil
	case MessageTypeRocketExploded:
		var payload RocketExploded
		if err := decodeStrict(raw, &payload); err != nil {
			return nil, "", validation("invalid RocketExploded payload: %v", err)
		}
		if payload.Reason == "" {
			return nil, "", validation("message.reason is required")
		}
		payloadJSON, err := canonicalJSON(payload)
		if err != nil {
			return nil, "", err
		}
		return payload, payloadJSON, nil
	case MessageTypeRocketMissionChanged:
		var payload RocketMissionChanged
		if err := decodeStrict(raw, &payload); err != nil {
			return nil, "", validation("invalid RocketMissionChanged payload: %v", err)
		}
		if payload.NewMission == "" {
			return nil, "", validation("message.newMission is required")
		}
		payloadJSON, err := canonicalJSON(payload)
		if err != nil {
			return nil, "", err
		}
		return payload, payloadJSON, nil
	default:
		return nil, "", validation("unknown message type %q", messageType)
	}
}

func decodeStrict(raw json.RawMessage, dst any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("payload must contain one JSON object")
	}
	return nil
}

func canonicalJSON(value any) (string, error) {
	bytes, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func validation(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrValidation, fmt.Sprintf(format, args...))
}
