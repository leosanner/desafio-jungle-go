//go:build integration

package httpserver

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/leosanner/desafio-jungle-go/internal/adapter/auth"
	"github.com/leosanner/desafio-jungle-go/internal/config"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAuthRealIdP(t *testing.T) {
	iss := os.Getenv("OIDC_ISSUER")
	if iss == "" {
		t.Skip("OIDC_ISSUER is not set")
	}
	waitDiscovery(t, iss)

	cfg := config.Config{
		HTTPAddr:           ":0",
		OIDCIssuer:         strings.TrimRight(iss, "/"),
		OIDCAudience:       getenv("OIDC_AUDIENCE", "wagering-api"),
		OIDCJWKSURL:        os.Getenv("OIDC_JWKS_URL"),
		OIDCInternalClient: getenv("OIDC_INTERNAL_CLIENT", "wagering-internal"),
	}
	v := auth.NewOIDCVerifier(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	s := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, v)

	providerA := clientCredentials(t, iss, "provider-a", "provider-a-secret")
	providerB := clientCredentials(t, iss, "provider-b", "provider-b-secret")
	internal := clientCredentials(t, iss, "wagering-internal", "internal-secret")

	t.Run("missing token", func(t *testing.T) {
		rec := do(s, http.MethodPost, "/wallets", "", "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d", rec.Code)
		}
	})
	t.Run("invalid token", func(t *testing.T) {
		rec := do(s, http.MethodPost, "/wallets", "not-a-jwt", "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d", rec.Code)
		}
	})
	t.Run("provider cannot open wallet", func(t *testing.T) {
		rec := do(s, http.MethodPost, "/wallets", providerA, `{"playerId":"x"}`)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d", rec.Code)
		}
	})
	t.Run("internal reaches wallet stub", func(t *testing.T) {
		rec := do(s, http.MethodPost, "/wallets", internal, `{"playerId":"x"}`)
		if rec.Code != http.StatusNotImplemented {
			t.Fatalf("status = %d", rec.Code)
		}
	})
	t.Run("provider isolation", func(t *testing.T) {
		rec := do(s, http.MethodGet, "/providers/provider-b/wagering/transactions/tx-1", providerA, "")
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d", rec.Code)
		}
		rec = do(s, http.MethodGet, "/providers/provider-a/wagering/transactions/tx-1", providerA, "")
		if rec.Code != http.StatusNotImplemented {
			t.Fatalf("own path status = %d", rec.Code)
		}
		rec = do(s, http.MethodGet, "/providers/provider-b/wagering/transactions/tx-1", providerB, "")
		if rec.Code != http.StatusNotImplemented {
			t.Fatalf("provider-b own path status = %d", rec.Code)
		}
	})
	t.Run("no financial side effect", func(t *testing.T) {
		before, ok := walletCount(t)
		rec := do(s, http.MethodPost, "/wallets", providerA, `{"playerId":"x"}`)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d", rec.Code)
		}
		if !ok {
			return
		}
		after, ok := walletCount(t)
		if !ok {
			t.Fatal("lost database between counts")
		}
		if before != after {
			t.Fatalf("wallets changed: %d → %d", before, after)
		}
	})
}

func do(s *Server, method, path, token, body string) *httptest.ResponseRecorder {
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rdr)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func walletCount(t *testing.T) (int, bool) {
	t.Helper()
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		return 0, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Logf("postgres: %v", err)
		return 0, false
	}
	defer pool.Close()
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM wagering.wallets`).Scan(&n); err != nil {
		t.Logf("wallets count: %v", err)
		return 0, false
	}
	return n, true
}

func waitDiscovery(t *testing.T, issuer string) {
	t.Helper()
	u := strings.TrimRight(issuer, "/") + "/.well-known/openid-configuration"
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(u)
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			return
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("oidc discovery not ready: %s", u)
}

func clientCredentials(t *testing.T, issuer, clientID, secret string) string {
	t.Helper()
	tokenURL := strings.TrimRight(issuer, "/") + "/protocol/openid-connect/token"
	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {clientID},
		"client_secret": {secret},
	}
	resp, err := http.PostForm(tokenURL, form)
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("token status %d: %s", resp.StatusCode, body)
	}
	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("token json: %v", err)
	}
	if out.AccessToken == "" {
		t.Fatal("empty access_token")
	}
	return out.AccessToken
}

func getenv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
