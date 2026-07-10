package domain

import (
	"strings"
	"testing"
)

func TestCanonicalHashIgnoresPayloadFormatting(t *testing.T) {
	first := `{
		"metadata":{"channel":"rocket-1","messageNumber":2,"messageTime":"2022-02-02T19:40:05.86337+01:00","messageType":"RocketSpeedIncreased"},
		"message":{"by":3000}
	}`
	second := `{
		"metadata":{"channel":"rocket-1","messageNumber":2,"messageTime":"2022-02-02T19:40:05.863370+01:00","messageType":"RocketSpeedIncreased"},
		"message":{
			"by": 3000
		}
	}`

	firstEvent := parseEvent(t, first)
	secondEvent := parseEvent(t, second)

	if firstEvent.PayloadJSON != secondEvent.PayloadJSON {
		t.Fatalf("payload JSON differs: %q != %q", firstEvent.PayloadJSON, secondEvent.PayloadJSON)
	}
	if firstEvent.EventHash != secondEvent.EventHash {
		t.Fatalf("event hash differs: %q != %q", firstEvent.EventHash, secondEvent.EventHash)
	}
}

func TestHashIncludesMessageType(t *testing.T) {
	base := `{
		"metadata":{"channel":"rocket-1","messageNumber":2,"messageTime":"2022-02-02T19:40:05.86337+01:00","messageType":"RocketSpeedIncreased"},
		"message":{"by":3000}
	}`
	changedType := strings.Replace(base, "RocketSpeedIncreased", "RocketSpeedDecreased", 1)

	firstEvent := parseEvent(t, base)
	secondEvent := parseEvent(t, changedType)

	if firstEvent.EventHash == secondEvent.EventHash {
		t.Fatal("expected different message types to produce different event hashes")
	}
}

func TestNewEventRejectsInvalidMessages(t *testing.T) {
	tests := map[string]string{
		"missing metadata": `{"message":{"by":1}}`,
		"unknown type": `{
			"metadata":{"channel":"rocket-1","messageNumber":1,"messageTime":"2022-02-02T19:39:05Z","messageType":"Nope"},
			"message":{}
		}`,
		"bad time": `{
			"metadata":{"channel":"rocket-1","messageNumber":1,"messageTime":"not-time","messageType":"RocketLaunched"},
			"message":{"type":"Falcon-9","launchSpeed":500,"mission":"ARTEMIS"}
		}`,
		"invalid launch speed": `{
			"metadata":{"channel":"rocket-1","messageNumber":1,"messageTime":"2022-02-02T19:39:05Z","messageType":"RocketLaunched"},
			"message":{"type":"Falcon-9","launchSpeed":-1,"mission":"ARTEMIS"}
		}`,
		"negative delta": `{
			"metadata":{"channel":"rocket-1","messageNumber":2,"messageTime":"2022-02-02T19:39:05Z","messageType":"RocketSpeedIncreased"},
			"message":{"by":-1}
		}`,
		"invalid message number": `{
			"metadata":{"channel":"rocket-1","messageNumber":0,"messageTime":"2022-02-02T19:39:05Z","messageType":"RocketLaunched"},
			"message":{"type":"Falcon-9","launchSpeed":500,"mission":"ARTEMIS"}
		}`,
	}

	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			envelope, err := ParseEnvelope(strings.NewReader(body))
			if err == nil {
				_, err = NewEvent(envelope)
			}
			if err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestNewEventAllowsZeroLaunchSpeed(t *testing.T) {
	parseEvent(t, `{
		"metadata":{"channel":"rocket-1","messageNumber":1,"messageTime":"2022-02-02T19:39:05Z","messageType":"RocketLaunched"},
		"message":{"type":"Falcon-9","launchSpeed":0,"mission":"ARTEMIS"}
	}`)
}

func parseEvent(t *testing.T, body string) Event {
	t.Helper()
	envelope, err := ParseEnvelope(strings.NewReader(body))
	if err != nil {
		t.Fatalf("parse envelope: %v", err)
	}
	event, err := NewEvent(envelope)
	if err != nil {
		t.Fatalf("new event: %v", err)
	}
	return event
}
