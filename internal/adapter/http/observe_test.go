package httpserver

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	metricsadapter "github.com/leosanner/desafio-jungle-go/internal/adapter/metrics"
	"github.com/leosanner/desafio-jungle-go/internal/app"
	"github.com/leosanner/desafio-jungle-go/internal/config"
)

func TestCorrelationIDEchoedAndLogged(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{ReplaceAttr: config.RedactLogAttr}))
	recMetrics := &app.RecordingMetrics{}
	s := New(config.Config{HTTPAddr: ":0"}, log, nil, rejectTokens{}, stubService(), recMetrics)

	const token = "super-secret-token-value"
	req := httptest.NewRequest(http.MethodPost, "/wagering/transactions", strings.NewReader(`{"money":{"amount":"25.00","currency":"BRL"}}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set(correlationHeader, "corr-from-client")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Header().Get(correlationHeader) != "corr-from-client" {
		t.Fatalf("X-Correlation-Id = %q", rec.Header().Get(correlationHeader))
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rec.Code)
	}
	logs := buf.String()
	if !strings.Contains(logs, `"correlationId":"corr-from-client"`) {
		t.Fatalf("missing correlation in logs: %s", logs)
	}
	if strings.Contains(logs, token) {
		t.Fatal("bearer token leaked into logs")
	}
	if strings.Contains(logs, "25.00") {
		t.Fatal("money amount leaked into logs")
	}
	if len(recMetrics.HTTP) != 1 {
		t.Fatalf("http observations = %d", len(recMetrics.HTTP))
	}
	if recMetrics.HTTP[0].Status != http.StatusUnauthorized {
		t.Fatalf("observed status = %d", recMetrics.HTTP[0].Status)
	}
	if recMetrics.HTTP[0].Pattern != "POST /wagering/transactions" {
		t.Fatalf("pattern = %q", recMetrics.HTTP[0].Pattern)
	}
}

func TestCorrelationIDGeneratedWhenMissing(t *testing.T) {
	t.Parallel()
	s := testServer(t, nil)
	req := httptest.NewRequest(http.MethodPost, "/wallets", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	got := rec.Header().Get(correlationHeader)
	if got == "" {
		t.Fatal("expected generated X-Correlation-Id")
	}
	if !validCorrelationID(got) {
		t.Fatalf("invalid generated id %q", got)
	}
}

func TestHealthAndMetricsSkipHTTPMetrics(t *testing.T) {
	t.Parallel()
	recMetrics := &app.RecordingMetrics{}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := New(config.Config{HTTPAddr: ":0"}, log, nil, rejectTokens{}, stubService(), recMetrics)
	for _, path := range []string{"/health/live", "/health/ready"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
	}
	if len(recMetrics.HTTP) != 0 {
		t.Fatalf("probes recorded: %+v", recMetrics.HTTP)
	}
}

func TestMetricsEndpointPublic(t *testing.T) {
	t.Parallel()
	p := metricsadapter.New()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := New(config.Config{HTTPAddr: ":0"}, log, nil, rejectTokens{}, stubService(), p)
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "wagering_reconciliation_divergences_total") {
		t.Fatalf("catalog missing: %s", rec.Body.String())
	}
}

func TestRedactLogAttrDeniesSecretsAndMoney(t *testing.T) {
	t.Parallel()
	cases := []string{"authorization", "token", "access_token", "password", "secret", "payload", "money", "amount"}
	for _, key := range cases {
		got := config.RedactLogAttr(nil, slog.String(key, "leak"))
		if got.Value.String() != "[redacted]" {
			t.Errorf("%s = %q", key, got.Value.String())
		}
	}
	kept := config.RedactLogAttr(nil, slog.String("correlationId", "c1"))
	if kept.Value.String() != "c1" {
		t.Errorf("correlationId redacted")
	}
}

func TestJSONLogsDoNotEncodeAuthorizationKey(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{ReplaceAttr: config.RedactLogAttr}))
	log.Info("auth", "authorization", "Bearer leak", "correlationId", "c1")
	var payload map[string]any
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["authorization"] != "[redacted]" {
		t.Fatalf("authorization = %v", payload["authorization"])
	}
	if payload["correlationId"] != "c1" {
		t.Fatalf("correlationId = %v", payload["correlationId"])
	}
}
