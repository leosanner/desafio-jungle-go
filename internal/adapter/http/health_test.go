package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/leosanner/desafio-jungle-go/internal/adapter/auth"
	"github.com/leosanner/desafio-jungle-go/internal/app"
	"github.com/leosanner/desafio-jungle-go/internal/config"
)

type fakeChecker struct {
	name string
	err  error
}

func (f fakeChecker) Name() string { return f.name }

func (f fakeChecker) Check(context.Context) error { return f.err }

func testServer(t *testing.T, checkers Checkers) *Server {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(config.Config{HTTPAddr: ":0"}, log, checkers, rejectTokens{}, stubService())
}

type rejectTokens struct{}

func (rejectTokens) Verify(context.Context, string) (app.Actor, error) {
	return app.Actor{}, auth.ErrUnauthenticated
}

func TestLiveAlwaysOK(t *testing.T) {
	t.Parallel()
	s := testServer(t, nil)

	req := httptest.NewRequest(http.MethodGet, "/health/live", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
	var body statusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json: %v", err)
	}
	if body.Status != "ok" {
		t.Errorf("status = %q", body.Status)
	}
}

func TestLiveOKDuringShutdown(t *testing.T) {
	t.Parallel()
	s := testServer(t, nil)
	s.shuttingDown.Store(true)

	req := httptest.NewRequest(http.MethodGet, "/health/live", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestReadyOK(t *testing.T) {
	t.Parallel()
	s := testServer(t, Checkers{
		fakeChecker{name: "postgres"},
		fakeChecker{name: "sqs"},
	})

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body statusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json: %v", err)
	}
	if body.Status != "ok" {
		t.Errorf("status = %q", body.Status)
	}
	if body.Checks != nil {
		t.Errorf("checks = %v, want omitted", body.Checks)
	}
}

func TestReadyUnavailable(t *testing.T) {
	t.Parallel()
	s := testServer(t, Checkers{
		fakeChecker{name: "postgres", err: errors.New("connection refused")},
		fakeChecker{name: "sqs"},
	})

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	var body statusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json: %v", err)
	}
	if body.Status != "unavailable" {
		t.Errorf("status = %q", body.Status)
	}
	if body.Checks["postgres"] != "connection refused" {
		t.Errorf("postgres check = %q", body.Checks["postgres"])
	}
	if body.Checks["sqs"] != "ok" {
		t.Errorf("sqs check = %q", body.Checks["sqs"])
	}
}

func TestReadyBothFailed(t *testing.T) {
	t.Parallel()
	s := testServer(t, Checkers{
		fakeChecker{name: "postgres", err: errors.New("down")},
		fakeChecker{name: "sqs", err: errors.New("missing queue")},
	})

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	var body statusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json: %v", err)
	}
	if body.Checks["postgres"] != "down" || body.Checks["sqs"] != "missing queue" {
		t.Errorf("checks = %v", body.Checks)
	}
}

func TestReadyUnavailableWhenShuttingDown(t *testing.T) {
	t.Parallel()
	s := testServer(t, Checkers{
		fakeChecker{name: "postgres"},
		fakeChecker{name: "sqs"},
	})
	s.shuttingDown.Store(true)

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	var body statusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json: %v", err)
	}
	if body.Status != "unavailable" {
		t.Errorf("status = %q", body.Status)
	}
	if body.Checks["shutdown"] != "in progress" {
		t.Errorf("checks = %v", body.Checks)
	}
}
