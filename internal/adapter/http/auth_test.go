package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/leosanner/desafio-jungle-go/internal/adapter/auth"
	"github.com/leosanner/desafio-jungle-go/internal/app"
	"github.com/leosanner/desafio-jungle-go/internal/config"
)

type staticTokens struct {
	actor app.Actor
	err   error
}

func (s staticTokens) Verify(context.Context, string) (app.Actor, error) {
	return s.actor, s.err
}

func authServer(t *testing.T, tokens TokenVerifier) *Server {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(config.Config{HTTPAddr: ":0"}, log, nil, tokens, stubService())
}

func TestHealthStaysPublic(t *testing.T) {
	t.Parallel()
	s := authServer(t, rejectTokens{})

	req := httptest.NewRequest(http.MethodGet, "/health/live", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("live status = %d", rec.Code)
	}
}

func TestBusinessRouteMissingToken(t *testing.T) {
	t.Parallel()
	s := authServer(t, rejectTokens{})

	req := httptest.NewRequest(http.MethodPost, "/wallets", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	assertErrorCode(t, rec, "unauthenticated")
}

func TestBusinessRouteInvalidToken(t *testing.T) {
	t.Parallel()
	s := authServer(t, rejectTokens{})

	req := httptest.NewRequest(http.MethodPost, "/wallets", nil)
	req.Header.Set("Authorization", "Bearer not-a-jwt")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestBusinessRouteExpiredToken(t *testing.T) {
	t.Parallel()
	s := authServer(t, staticTokens{err: fmtUnauthenticated("token expired")})

	req := httptest.NewRequest(http.MethodGet, "/providers/provider-a/wagering/transactions/tx-1", nil)
	req.Header.Set("Authorization", "Bearer expired")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestIdPUnavailableIs503(t *testing.T) {
	t.Parallel()
	s := authServer(t, staticTokens{err: auth.ErrUnavailable})

	req := httptest.NewRequest(http.MethodPost, "/wallets", nil)
	req.Header.Set("Authorization", "Bearer x")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	assertErrorCode(t, rec, "unavailable")
}

func TestWalletRestrictedToInternal(t *testing.T) {
	t.Parallel()
	provider := authServer(t, staticTokens{actor: app.Actor{ClientID: "provider-a", ProviderID: "provider-a"}})
	internal := authServer(t, staticTokens{actor: app.Actor{ClientID: "wagering-internal", Internal: true}})

	req := httptest.NewRequest(http.MethodPost, "/wallets", nil)
	req.Header.Set("Authorization", "Bearer t")
	rec := httptest.NewRecorder()
	provider.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("provider status = %d, want 403", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/wallets", nil)
	req.Header.Set("Authorization", "Bearer t")
	rec = httptest.NewRecorder()
	internal.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("internal status = %d, want 400", rec.Code)
	}
}

func TestProviderIsolationOnPath(t *testing.T) {
	t.Parallel()
	s := authServer(t, staticTokens{actor: app.Actor{ClientID: "provider-a", ProviderID: "provider-a"}})

	req := httptest.NewRequest(http.MethodGet, "/providers/provider-b/wagering/transactions/tx-1", nil)
	req.Header.Set("Authorization", "Bearer t")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/providers/provider-a/wagering/transactions/tx-1", nil)
	req.Header.Set("Authorization", "Bearer t")
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestInternalCanReadAnyProviderPath(t *testing.T) {
	t.Parallel()
	s := authServer(t, staticTokens{actor: app.Actor{ClientID: "wagering-internal", Internal: true}})

	req := httptest.NewRequest(http.MethodGet, "/providers/provider-b/wagering/transactions/tx-1", nil)
	req.Header.Set("Authorization", "Bearer t")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestPostWageringRejectsInternalAndBodyMismatch(t *testing.T) {
	t.Parallel()
	internal := authServer(t, staticTokens{actor: app.Actor{ClientID: "wagering-internal", Internal: true}})
	req := httptest.NewRequest(http.MethodPost, "/wagering/transactions", strings.NewReader(`{"providerId":"provider-a"}`))
	req.Header.Set("Authorization", "Bearer t")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	internal.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("internal status = %d, want 403", rec.Code)
	}

	provider := authServer(t, staticTokens{actor: app.Actor{ClientID: "provider-a", ProviderID: "provider-a"}})
	req = httptest.NewRequest(http.MethodPost, "/wagering/transactions", strings.NewReader(`{"providerId":"provider-b"}`))
	req.Header.Set("Authorization", "Bearer t")
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	provider.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("mismatch status = %d, want 403", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/wagering/transactions", strings.NewReader(`{"providerId":"provider-a"}`))
	req.Header.Set("Authorization", "Bearer t")
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	provider.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("match status = %d, want 400", rec.Code)
	}
}

func TestDeniedAccessDoesNotReachHandler(t *testing.T) {
	t.Parallel()
	called := false
	inner := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })
	s := authServer(t, rejectTokens{})
	h := s.authenticate(inner)

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if called {
		t.Fatal("handler ran without a token")
	}

	req = httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Authorization", "Bearer bad")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if called {
		t.Fatal("handler ran with an invalid token")
	}
}

func TestForbiddenBodyDoesNotLeak(t *testing.T) {
	t.Parallel()
	s := authServer(t, staticTokens{actor: app.Actor{ClientID: "provider-a", ProviderID: "provider-a"}})
	req := httptest.NewRequest(http.MethodGet, "/providers/provider-b/wagering/transactions/secret-tx", nil)
	req.Header.Set("Authorization", "Bearer t")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "secret-tx") || strings.Contains(body, "provider-b") {
		t.Fatalf("body leaked identifiers: %s", body)
	}
}

func TestMalformedWageringJSON(t *testing.T) {
	t.Parallel()
	s := authServer(t, staticTokens{actor: app.Actor{ClientID: "provider-a", ProviderID: "provider-a"}})
	req := httptest.NewRequest(http.MethodPost, "/wagering/transactions", bytes.NewBufferString("{"))
	req.Header.Set("Authorization", "Bearer t")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func assertErrorCode(t *testing.T, rec *httptest.ResponseRecorder, want string) {
	t.Helper()
	var body errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json: %v", err)
	}
	if body.Error != want {
		t.Fatalf("error = %q, want %q", body.Error, want)
	}
}

func fmtUnauthenticated(reason string) error {
	return errors.Join(auth.ErrUnauthenticated, errors.New(reason))
}
