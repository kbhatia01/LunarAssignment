package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"lunar-rockets/src/internal/domain"
	"lunar-rockets/src/internal/repository/sqlite"
	"lunar-rockets/src/internal/service"
)

func TestHTTPMessageLifecycleUsesResponseEnvelopes(t *testing.T) {
	handler := realHandler(t)

	launch := `{
		"metadata":{"channel":"rocket-1","messageNumber":1,"messageTime":"2022-02-02T19:39:05Z","messageType":"RocketLaunched"},
		"message":{"type":"Falcon-9","launchSpeed":500,"mission":"ARTEMIS"}
	}`

	response := request(handler, http.MethodPost, "/messages", launch)
	if response.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", response.Code, response.Body.String())
	}
	var created map[string]map[string]any
	decode(t, response, &created)
	if created["data"]["status"] != "created" || created["data"]["materialized"] != true {
		t.Fatalf("unexpected created response: %#v", created)
	}

	response = request(handler, http.MethodPost, "/messages", launch)
	if response.Code != http.StatusOK {
		t.Fatalf("expected duplicate 200, got %d: %s", response.Code, response.Body.String())
	}
	var duplicate map[string]map[string]any
	decode(t, response, &duplicate)
	if duplicate["data"]["status"] != "duplicate" || duplicate["data"]["materialized"] != false {
		t.Fatalf("unexpected duplicate response: %#v", duplicate)
	}

	conflict := bytes.ReplaceAll([]byte(launch), []byte("ARTEMIS"), []byte("GEMINI"))
	response = request(handler, http.MethodPost, "/messages", string(conflict))
	if response.Code != http.StatusOK {
		t.Fatalf("expected conflict ignored 200, got %d: %s", response.Code, response.Body.String())
	}
	var conflictIgnored map[string]map[string]any
	decode(t, response, &conflictIgnored)
	if conflictIgnored["data"]["status"] != "conflict_ignored" || conflictIgnored["data"]["materialized"] != false {
		t.Fatalf("unexpected conflict ignored response: %#v", conflictIgnored)
	}
}

func TestHTTPDuplicateUsesCanonicalPayloadJSON(t *testing.T) {
	handler := realHandler(t)

	launch := `{
		"metadata":{"channel":"rocket-canonical","messageNumber":1,"messageTime":"2022-02-02T19:39:05Z","messageType":"RocketLaunched"},
		"message":{"type":"Falcon-9","launchSpeed":500,"mission":"ARTEMIS"}
	}`
	if response := request(handler, http.MethodPost, "/messages", launch); response.Code != http.StatusCreated {
		t.Fatalf("expected launch 201, got %d: %s", response.Code, response.Body.String())
	}

	first := `{
		"metadata":{"channel":"rocket-canonical","messageNumber":2,"messageTime":"2022-02-02T19:40:05Z","messageType":"RocketSpeedIncreased"},
		"message":{"by":3000}
	}`
	second := `{
		"metadata": {
			"channel": "rocket-canonical",
			"messageNumber": 2,
			"messageTime": "2022-02-02T19:40:05Z",
			"messageType": "RocketSpeedIncreased"
		},
		"message": {
			"by": 3000
		}
	}`

	if response := request(handler, http.MethodPost, "/messages", first); response.Code != http.StatusCreated {
		t.Fatalf("expected speed 201, got %d: %s", response.Code, response.Body.String())
	}
	response := request(handler, http.MethodPost, "/messages", second)
	if response.Code != http.StatusOK {
		t.Fatalf("expected formatted duplicate 200, got %d: %s", response.Code, response.Body.String())
	}
	var duplicate map[string]map[string]any
	decode(t, response, &duplicate)
	if duplicate["data"]["status"] != "duplicate" {
		t.Fatalf("expected canonical duplicate, got %#v", duplicate)
	}
}

func TestHTTPConcurrentOutOfOrderDeliveryWithDuplicates(t *testing.T) {
	handler := realHandler(t)

	messages := []string{
		`{"metadata":{"channel":"rocket-concurrent","messageNumber":3,"messageTime":"2022-02-02T19:41:05Z","messageType":"RocketSpeedDecreased"},"message":{"by":2500}}`,
		`{"metadata":{"channel":"rocket-concurrent","messageNumber":2,"messageTime":"2022-02-02T19:40:05Z","messageType":"RocketSpeedIncreased"},"message":{"by":3000}}`,
		`{"metadata":{"channel":"rocket-concurrent","messageNumber":2,"messageTime":"2022-02-02T19:40:05Z","messageType":"RocketSpeedIncreased"},"message":{"by":3000}}`,
		`{"metadata":{"channel":"rocket-concurrent","messageNumber":1,"messageTime":"2022-02-02T19:39:05Z","messageType":"RocketLaunched"},"message":{"type":"Falcon-9","launchSpeed":500,"mission":"ARTEMIS"}}`,
		`{"metadata":{"channel":"rocket-concurrent","messageNumber":1,"messageTime":"2022-02-02T19:39:05Z","messageType":"RocketLaunched"},"message":{"type":"Falcon-9","launchSpeed":500,"mission":"ARTEMIS"}}`,
	}

	errCh := make(chan string, len(messages))

	for _, body := range messages {
		body := body
		go func() {
			response := request(handler, http.MethodPost, "/messages", body)
			if response.Code != http.StatusCreated && response.Code != http.StatusOK {
				errCh <- response.Body.String()
				return
			}
			errCh <- ""
		}()
	}

	for range messages {
		if err := <-errCh; err != "" {
			t.Fatalf("concurrent request failed: %s", err)
		}
	}

	response := request(handler, http.MethodGet, "/rockets/rocket-concurrent", "")
	if response.Code != http.StatusOK {
		t.Fatalf("expected get 200, got %d: %s", response.Code, response.Body.String())
	}

	var result dataEnvelope[rocketResponse]
	decode(t, response, &result)

	if result.Data.LastMessageNumber != 3 {
		t.Fatalf("expected last message 3, got %d", result.Data.LastMessageNumber)
	}
	if result.Data.PendingEvents != 0 {
		t.Fatalf("expected no pending events, got %d", result.Data.PendingEvents)
	}
	if result.Data.Speed != 1000 {
		t.Fatalf("expected speed 1000, got %d", result.Data.Speed)
	}
	if result.Data.Mission != "ARTEMIS" {
		t.Fatalf("expected mission ARTEMIS, got %q", result.Data.Mission)
	}
}

func TestHTTPListGetAndSortingMeta(t *testing.T) {
	handler := realHandler(t)
	messages := []string{
		`{"metadata":{"channel":"rocket-b","messageNumber":1,"messageTime":"2022-02-02T19:39:05Z","messageType":"RocketLaunched"},"message":{"type":"Falcon-9","launchSpeed":500,"mission":"ARTEMIS"}}`,
		`{"metadata":{"channel":"rocket-b","messageNumber":2,"messageTime":"2022-02-02T19:40:05Z","messageType":"RocketSpeedIncreased"},"message":{"by":100}}`,
		`{"metadata":{"channel":"rocket-b","messageNumber":4,"messageTime":"2022-02-02T19:42:05Z","messageType":"RocketSpeedIncreased"},"message":{"by":100}}`,
		`{"metadata":{"channel":"rocket-a","messageNumber":1,"messageTime":"2022-02-02T19:39:05Z","messageType":"RocketLaunched"},"message":{"type":"Falcon-9","launchSpeed":500,"mission":"ARTEMIS"}}`,
	}
	for _, body := range messages {
		response := request(handler, http.MethodPost, "/messages", body)
		if response.Code != http.StatusCreated {
			t.Fatalf("post failed: %d %s", response.Code, response.Body.String())
		}
	}

	response := request(handler, http.MethodGet, "/rockets?sort=mission&order=asc", "")
	if response.Code != http.StatusOK {
		t.Fatalf("expected list 200, got %d: %s", response.Code, response.Body.String())
	}
	var list listRocketsResponse
	decode(t, response, &list)
	if list.Meta.Count != 2 || list.Meta.Sort != "mission" || list.Meta.Order != "asc" {
		t.Fatalf("unexpected meta: %#v", list.Meta)
	}
	if list.Data[0].Channel != "rocket-a" || list.Data[1].Channel != "rocket-b" {
		t.Fatalf("expected channel tie-breaker, got %#v", list.Data)
	}
	if list.Data[1].LastMessageNumber != 2 || list.Data[1].PendingEvents != 1 {
		t.Fatalf("unexpected pending state: %#v", list.Data[1])
	}

	response = request(handler, http.MethodGet, "/rockets/rocket-a", "")
	if response.Code != http.StatusOK {
		t.Fatalf("expected get 200, got %d: %s", response.Code, response.Body.String())
	}
	var one dataEnvelope[rocketResponse]
	decode(t, response, &one)
	if one.Data.Channel != "rocket-a" || one.Data.ExplosionReason != nil {
		t.Fatalf("unexpected get response: %#v", one)
	}
}

func TestHTTPErrorMappings(t *testing.T) {
	handler := realHandler(t)

	response := request(handler, http.MethodPost, "/messages", `{"bad":true}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", response.Code)
	}
	var validation errorEnvelope
	decode(t, response, &validation)
	if validation.Error.Code != "validation_error" {
		t.Fatalf("unexpected validation error: %#v", validation)
	}

	response = request(handler, http.MethodPost, "/messages", `{
		"metadata":{"channel":"rocket-1","messageNumber":1,"messageTime":"2022-02-02T19:39:05Z","messageType":"RocketLaunched"},
		"message":{"type":"Falcon-9","launchSpeed":-1,"mission":"ARTEMIS"}
	}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid launch speed 400, got %d", response.Code)
	}

	response = request(handler, http.MethodGet, "/rockets/missing", "")
	if response.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", response.Code)
	}

	response = request(handler, http.MethodGet, "/rockets?sort=bad", "")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid sort 400, got %d", response.Code)
	}
}

func TestHTTPHealthResponses(t *testing.T) {
	handler := realHandler(t)
	response := request(handler, http.MethodGet, "/healthz", "")
	if response.Code != http.StatusOK {
		t.Fatalf("expected health 200, got %d", response.Code)
	}
	var health healthResponse
	decode(t, response, &health)
	if health.Status != "ok" || health.Checks["sqlite"] != "ok" {
		t.Fatalf("unexpected health response: %#v", health)
	}

	failing := New(noopIngest{}, noopQuery{}, failingHealth{})
	response = request(failing.Routes(), http.MethodGet, "/healthz", "")
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected health failure 503, got %d", response.Code)
	}
}

func realHandler(t *testing.T) http.Handler {
	t.Helper()
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "rockets.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	ingest := service.NewIngestMessageService(db, service.RealClock{})
	query := service.NewRocketQueryService(db)
	health := service.NewHealthService(db)
	return New(ingest, query, health).Routes()
}

func request(handler http.Handler, method string, path string, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	return res
}

func decode(t *testing.T, response *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.Unmarshal(response.Body.Bytes(), target); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, response.Body.String())
	}
}

type noopIngest struct{}

func (noopIngest) Handle(ctx context.Context, command service.IngestMessageCommand) (service.IngestMessageResult, error) {
	return service.IngestMessageResult{}, nil
}

type noopQuery struct{}

func (noopQuery) List(ctx context.Context, query service.ListRocketsQuery) (service.ListRocketsResult, error) {
	return service.ListRocketsResult{}, nil
}

func (noopQuery) Get(ctx context.Context, channel string) (domain.RocketState, error) {
	return domain.RocketState{}, service.ErrNotFound
}

type failingHealth struct{}

func (failingHealth) Check(ctx context.Context) (service.HealthResult, error) {
	return service.HealthResult{}, errors.New("database down")
}
