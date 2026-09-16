package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"github.com/leosanner/desafio-jungle-go/internal/app"
	"github.com/leosanner/desafio-jungle-go/internal/config"

	"github.com/coreos/go-oidc/v3/oidc"
)

const maxJWKSBytes = 1 << 20

// OIDCVerifier validates Bearer access tokens against the IdP JWKS.
type OIDCVerifier struct {
	verifier       *oidc.IDTokenVerifier
	jwksURL        string
	internalClient string
	client         *http.Client
	log            *slog.Logger
}

// NewOIDCVerifier constructs a verifier. JWKS is fetched on Check / first Verify,
// not in this constructor.
func NewOIDCVerifier(cfg config.Config, log *slog.Logger) *OIDCVerifier {
	issuer := strings.TrimRight(strings.TrimSpace(cfg.OIDCIssuer), "/")
	jwks := strings.TrimSpace(cfg.OIDCJWKSURL)
	if jwks == "" {
		jwks = issuer + "/protocol/openid-connect/certs"
	}
	keySet := oidc.NewRemoteKeySet(context.Background(), jwks)
	return &OIDCVerifier{
		verifier: oidc.NewVerifier(issuer, keySet, &oidc.Config{
			ClientID:             cfg.OIDCAudience,
			SupportedSigningAlgs: []string{oidc.RS256},
		}),
		jwksURL:        jwks,
		internalClient: cfg.OIDCInternalClient,
		client:         http.DefaultClient,
		log:            log,
	}
}

// Check fetches JWKS and fails if the IdP is unreachable or the key set is empty.
func (v *OIDCVerifier) Check(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.jwksURL, nil)
	if err != nil {
		return fmt.Errorf("oidc: jwks request: %w", err)
	}
	resp, err := v.client.Do(req)
	if err != nil {
		return fmt.Errorf("oidc: jwks: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("oidc: jwks status %d", resp.StatusCode)
	}
	var set struct {
		Keys []json.RawMessage `json:"keys"`
	}
	limited := io.LimitReader(resp.Body, maxJWKSBytes)
	if err := json.NewDecoder(limited).Decode(&set); err != nil {
		return fmt.Errorf("oidc: jwks decode: %w", err)
	}
	if len(set.Keys) == 0 {
		return fmt.Errorf("oidc: jwks empty key set")
	}
	if v.log != nil {
		v.log.Info("oidc jwks reachable")
	}
	return nil
}

// Verify checks signature, issuer, audience, expiry and RS256, then maps azp to Actor.
func (v *OIDCVerifier) Verify(ctx context.Context, rawJWT string) (app.Actor, error) {
	tok, err := v.verifier.Verify(ctx, rawJWT)
	if err != nil {
		return app.Actor{}, classifyVerify(err)
	}
	var claims struct {
		Azp      string `json:"azp"`
		ClientID string `json:"client_id"`
	}
	if err := tok.Claims(&claims); err != nil {
		return app.Actor{}, fmt.Errorf("%w: claims: %w", ErrUnauthenticated, err)
	}
	clientID := claims.Azp
	if clientID == "" {
		clientID = claims.ClientID
	}
	if clientID == "" {
		return app.Actor{}, fmt.Errorf("%w: missing azp", ErrUnauthenticated)
	}
	actor := app.Actor{
		Subject:  tok.Subject,
		ClientID: clientID,
	}
	if clientID == v.internalClient {
		actor.Internal = true
		return actor, nil
	}
	actor.ProviderID = clientID
	return actor, nil
}

func classifyVerify(err error) error {
	var expired *oidc.TokenExpiredError
	if errors.As(err, &expired) {
		return fmt.Errorf("%w: token expired", ErrUnauthenticated)
	}
	if unavailableJWKS(err) {
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return fmt.Errorf("%w: %w", ErrUnauthenticated, err)
}

func unavailableJWKS(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, s := range []string{
		"failed to get keys",
		"fetching keys",
		"connection refused",
		"i/o timeout",
		"no such host",
		"network is unreachable",
	} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}
