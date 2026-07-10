package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"lunar-rockets/src/internal/domain"
	"lunar-rockets/src/internal/service"
)

type IngestService interface {
	Handle(ctx context.Context, command service.IngestMessageCommand) (service.IngestMessageResult, error)
}

type QueryService interface {
	List(ctx context.Context, query service.ListRocketsQuery) (service.ListRocketsResult, error)
	Get(ctx context.Context, channel string) (domain.RocketState, error)
}

type HealthService interface {
	Check(ctx context.Context) (service.HealthResult, error)
}

type API struct {
	ingest IngestService
	query  QueryService
	health HealthService
}

func New(ingest IngestService, query QueryService, health HealthService) *API {
	return &API{
		ingest: ingest,
		query:  query,
		health: health,
	}
}

func (a *API) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /messages", a.postMessage)
	mux.HandleFunc("GET /rockets", a.listRockets)
	mux.HandleFunc("GET /rockets/{channel}", a.getRocket)
	mux.HandleFunc("GET /healthz", a.healthz)
	return a.observeRequests(mux)
}

func (a *API) postMessage(w http.ResponseWriter, r *http.Request) {
	envelope, err := domain.ParseEnvelope(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeAppError(w, err)
		return
	}

	result, err := a.ingest.Handle(r.Context(), service.IngestMessageCommand{
		Metadata: envelope.Metadata,
		Message:  envelope.Message,
	})
	if err != nil {
		writeAppError(w, err)
		return
	}

	response := dataEnvelope[ingestResponse]{
		Data: ingestResponse{
			Status:        string(result.Status),
			Channel:       result.Channel,
			MessageNumber: result.MessageNumber,
			Materialized:  result.Materialized,
		},
	}
	if result.Status == service.IngestStatusDuplicate || result.Status == service.IngestStatusConflictingDuplicateIgnored {
		writeJSON(w, http.StatusOK, response)
		return
	}
	writeJSON(w, http.StatusCreated, response)
}

func (a *API) listRockets(w http.ResponseWriter, r *http.Request) {
	values := r.URL.Query()
	result, err := a.query.List(r.Context(), service.ListRocketsQuery{
		Sort:  values.Get("sort"),
		Order: values.Get("order"),
	})
	if err != nil {
		writeAppError(w, err)
		return
	}

	rockets := make([]rocketResponse, 0, len(result.Rockets))
	for _, state := range result.Rockets {
		rockets = append(rockets, rocketFromDomain(state))
	}
	writeJSON(w, http.StatusOK, listRocketsResponse{
		Data: rockets,
		Meta: listMeta{
			Count: len(rockets),
			Sort:  result.Sort,
			Order: result.Order,
		},
	})
}

func (a *API) getRocket(w http.ResponseWriter, r *http.Request) {
	state, err := a.query.Get(r.Context(), r.PathValue("channel"))
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dataEnvelope[rocketResponse]{Data: rocketFromDomain(state)})
}

func (a *API) healthz(w http.ResponseWriter, r *http.Request) {
	result, err := a.health.Check(r.Context())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, errorEnvelope{
			Error: apiError{
				Code:    "service_unavailable",
				Message: "database is not ready",
			},
		})
		return
	}
	writeJSON(w, http.StatusOK, healthResponse{
		Status: result.Status,
		Checks: result.Checks,
	})
}

type dataEnvelope[T any] struct {
	Data T `json:"data"`
}

type ingestResponse struct {
	Status        string `json:"status"`
	Channel       string `json:"channel"`
	MessageNumber int64  `json:"messageNumber"`
	Materialized  bool   `json:"materialized"`
}

type listRocketsResponse struct {
	Data []rocketResponse `json:"data"`
	Meta listMeta         `json:"meta"`
}

type listMeta struct {
	Count int    `json:"count"`
	Sort  string `json:"sort"`
	Order string `json:"order"`
}

type rocketResponse struct {
	Channel           string  `json:"channel"`
	Type              string  `json:"type"`
	Mission           string  `json:"mission"`
	Speed             int64   `json:"speed"`
	Status            string  `json:"status"`
	ExplosionReason   *string `json:"explosionReason"`
	LastMessageNumber int64   `json:"lastMessageNumber"`
	LastMessageTime   string  `json:"lastMessageTime"`
	PendingEvents     int64   `json:"pendingEvents"`
	UpdatedAt         string  `json:"updatedAt"`
}

type healthResponse struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

type errorEnvelope struct {
	Error apiError `json:"error"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func rocketFromDomain(state domain.RocketState) rocketResponse {
	return rocketResponse{
		Channel:           state.Channel,
		Type:              state.Type,
		Mission:           state.Mission,
		Speed:             state.Speed,
		Status:            state.Status,
		ExplosionReason:   state.ExplosionReason,
		LastMessageNumber: state.LastMessageNumber,
		LastMessageTime:   state.LastMessageTime.Format(time.RFC3339Nano),
		PendingEvents:     state.PendingEvents,
		UpdatedAt:         state.UpdatedAt.Format(time.RFC3339Nano),
	}
}

func writeAppError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	code := "internal_error"
	message := "internal server error"

	switch {
	case errors.Is(err, domain.ErrValidation):
		status = http.StatusBadRequest
		code = "validation_error"
		message = trimValidationPrefix(err.Error())
	case errors.Is(err, service.ErrInvalidSort):
		status = http.StatusBadRequest
		code = "validation_error"
		message = err.Error()
	case errors.Is(err, service.ErrNotFound):
		status = http.StatusNotFound
		code = "not_found"
		message = "rocket not found"
	}

	writeJSON(w, status, errorEnvelope{
		Error: apiError{
			Code:    code,
			Message: message,
		},
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func (a *API) observeRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		recorder := &statusRecorder{
			ResponseWriter: w,
			status:         http.StatusOK,
		}
		next.ServeHTTP(recorder, r)
		duration := time.Since(start)
		slog.InfoContext(r.Context(), "http request completed",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", recorder.status),
			slog.Int64("duration_ms", duration.Milliseconds()),
		)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func trimValidationPrefix(message string) string {
	const prefix = "validation error: "
	if len(message) >= len(prefix) && message[:len(prefix)] == prefix {
		return message[len(prefix):]
	}
	return message
}
