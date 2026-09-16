//go:build integration

package auth_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/leosanner/desafio-jungle-go/internal/adapter/auth"
	"github.com/leosanner/desafio-jungle-go/internal/config"
)

func TestOIDCVerifierRealIdP(t *testing.T) {
	iss := os.Getenv("OIDC_ISSUER")
	if iss == "" {
		t.Skip("OIDC_ISSUER is not set")
	}
	waitDiscovery(t, iss)

	cfg := config.Config{
		OIDCIssuer:         strings.TrimRight(iss, "/"),
		OIDCAudience:       getenv("OIDC_AUDIENCE", "wagering-api"),
		OIDCJWKSURL:        os.Getenv("OIDC_JWKS_URL"),
		OIDCInternalClient: getenv("OIDC_INTERNAL_CLIENT", "wagering-internal"),
	}
	v := auth.NewOIDCVerifier(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := v.Check(context.Background()); err != nil {
		t.Fatalf("Check: %v", err)
	}

	token := clientCredentials(t, iss, "provider-a", "provider-a-secret")
	actor, err := v.Verify(context.Background(), token)
	if err != nil {
		t.Fatalf("Verify provider-a: %v", err)
	}
	if actor.Internal || actor.ProviderID != "provider-a" {
		t.Fatalf("actor = %+v", actor)
	}

	internal := clientCredentials(t, iss, "wagering-internal", "internal-secret")
	actor, err = v.Verify(context.Background(), internal)
	if err != nil {
		t.Fatalf("Verify internal: %v", err)
	}
	if !actor.Internal || actor.ProviderID != "" {
		t.Fatalf("internal actor = %+v", actor)
	}

	_, err = v.Verify(context.Background(), "not-a-jwt")
	if !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatalf("invalid token err = %v", err)
	}
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
