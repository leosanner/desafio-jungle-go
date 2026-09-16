package auth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/leosanner/desafio-jungle-go/internal/config"

	"github.com/coreos/go-oidc/v3/oidc"
	jose "github.com/go-jose/go-jose/v4"
)

const (
	testIssuer   = "http://issuer.example/realms/wagering"
	testAudience = "wagering-api"
	testInternal = "wagering-internal"
)

func TestVerifyAcceptsProviderToken(t *testing.T) {
	t.Parallel()
	priv, v := staticVerifier(t)
	raw := signClaims(t, priv, claims{
		iss: testIssuer, aud: testAudience, sub: "sa-a", azp: "provider-a",
		exp: time.Now().Add(time.Minute),
	})

	actor, err := v.Verify(context.Background(), raw)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if actor.Internal || actor.ProviderID != "provider-a" || actor.ClientID != "provider-a" {
		t.Fatalf("actor = %+v", actor)
	}
}

func TestVerifyAcceptsInternalToken(t *testing.T) {
	t.Parallel()
	priv, v := staticVerifier(t)
	raw := signClaims(t, priv, claims{
		iss: testIssuer, aud: testAudience, sub: "sa-int", azp: testInternal,
		exp: time.Now().Add(time.Minute),
	})

	actor, err := v.Verify(context.Background(), raw)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !actor.Internal || actor.ProviderID != "" {
		t.Fatalf("actor = %+v", actor)
	}
}

func TestVerifyRejectsExpired(t *testing.T) {
	t.Parallel()
	priv, v := staticVerifier(t)
	raw := signClaims(t, priv, claims{
		iss: testIssuer, aud: testAudience, sub: "sa-a", azp: "provider-a",
		exp: time.Now().Add(-time.Minute),
	})

	_, err := v.Verify(context.Background(), raw)
	if !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("err = %v, want %v", err, ErrUnauthenticated)
	}
}

func TestVerifyRejectsWrongIssuer(t *testing.T) {
	t.Parallel()
	priv, v := staticVerifier(t)
	raw := signClaims(t, priv, claims{
		iss: "http://other.example/realms/wagering", aud: testAudience, sub: "sa-a", azp: "provider-a",
		exp: time.Now().Add(time.Minute),
	})

	_, err := v.Verify(context.Background(), raw)
	if !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("err = %v, want %v", err, ErrUnauthenticated)
	}
}

func TestVerifyRejectsWrongAudience(t *testing.T) {
	t.Parallel()
	priv, v := staticVerifier(t)
	raw := signClaims(t, priv, claims{
		iss: testIssuer, aud: "other-api", sub: "sa-a", azp: "provider-a",
		exp: time.Now().Add(time.Minute),
	})

	_, err := v.Verify(context.Background(), raw)
	if !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("err = %v, want %v", err, ErrUnauthenticated)
	}
}

func TestVerifyRejectsHMAC(t *testing.T) {
	t.Parallel()
	_, v := staticVerifier(t)
	signer, err := jose.NewSigner(jose.SigningKey{
		Algorithm: jose.HS256,
		Key:       []byte("0123456789abcdef0123456789abcdef"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]any{
		"iss": testIssuer,
		"aud": testAudience,
		"sub": "sa-a",
		"azp": "provider-a",
		"exp": time.Now().Add(time.Minute).Unix(),
		"iat": time.Now().Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	jws, err := signer.Sign(payload)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := jws.CompactSerialize()
	if err != nil {
		t.Fatal(err)
	}

	_, err = v.Verify(context.Background(), raw)
	if !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("err = %v, want %v", err, ErrUnauthenticated)
	}
}

func TestVerifyRejectsNoneAlg(t *testing.T) {
	t.Parallel()
	_, v := staticVerifier(t)
	header, err := json.Marshal(map[string]string{"alg": "none", "typ": "JWT"})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]any{
		"iss": testIssuer,
		"aud": testAudience,
		"sub": "sa-a",
		"azp": "provider-a",
		"exp": time.Now().Add(time.Minute).Unix(),
		"iat": time.Now().Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	raw := b64URL(header) + "." + b64URL(payload) + "."
	_, err = v.Verify(context.Background(), raw)
	if !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("err = %v, want %v", err, ErrUnauthenticated)
	}
}

func TestVerifyRejectsMissingAzp(t *testing.T) {
	t.Parallel()
	priv, v := staticVerifier(t)
	raw := signClaims(t, priv, claims{
		iss: testIssuer, aud: testAudience, sub: "sa-a",
		exp: time.Now().Add(time.Minute),
	})

	_, err := v.Verify(context.Background(), raw)
	if !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("err = %v, want %v", err, ErrUnauthenticated)
	}
}

func TestCheckAndVerifyJWKSUnavailable(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	jwksURL := srv.URL
	srv.Close()

	v := NewOIDCVerifier(config.Config{
		OIDCIssuer:         testIssuer,
		OIDCAudience:       testAudience,
		OIDCJWKSURL:        jwksURL,
		OIDCInternalClient: testInternal,
	}, discardLogger())

	if err := v.Check(context.Background()); err == nil {
		t.Fatal("Check: want error")
	}

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	raw := signClaims(t, priv, claims{
		iss: testIssuer, aud: testAudience, sub: "sa-a", azp: "provider-a",
		exp: time.Now().Add(time.Minute),
	})
	_, err = v.Verify(context.Background(), raw)
	if !errors.Is(err, ErrUnavailable) && !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("err = %v, want unavailable or unauthenticated", err)
	}
}

func TestCheckOK(t *testing.T) {
	t.Parallel()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	jwk := jose.JSONWebKey{Key: &priv.PublicKey, Use: "sig", Algorithm: string(jose.RS256), KeyID: "k1"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{jwk}})
	}))
	t.Cleanup(srv.Close)

	v := NewOIDCVerifier(config.Config{
		OIDCIssuer:         testIssuer,
		OIDCAudience:       testAudience,
		OIDCJWKSURL:        srv.URL,
		OIDCInternalClient: testInternal,
	}, discardLogger())
	if err := v.Check(context.Background()); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

type claims struct {
	iss, aud, sub, azp string
	exp                time.Time
}

func staticVerifier(t *testing.T) (*rsa.PrivateKey, *OIDCVerifier) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	v := &OIDCVerifier{
		verifier: oidc.NewVerifier(testIssuer, &oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{&priv.PublicKey}}, &oidc.Config{
			ClientID:             testAudience,
			SupportedSigningAlgs: []string{oidc.RS256},
		}),
		internalClient: testInternal,
		client:         http.DefaultClient,
		log:            discardLogger(),
	}
	return priv, v
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func signClaims(t *testing.T, priv *rsa.PrivateKey, c claims) string {
	t.Helper()
	signer, err := jose.NewSigner(jose.SigningKey{
		Algorithm: jose.RS256,
		Key:       &jose.JSONWebKey{Key: priv, Algorithm: string(jose.RS256), KeyID: "k1"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	body := map[string]any{
		"iss": c.iss,
		"aud": c.aud,
		"sub": c.sub,
		"exp": c.exp.Unix(),
		"iat": time.Now().Unix(),
	}
	if c.azp != "" {
		body["azp"] = c.azp
	}
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	jws, err := signer.Sign(payload)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := jws.CompactSerialize()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func b64URL(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}
