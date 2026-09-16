package auth

import "errors"

// Sentinel errors for HTTP mapping. Never wrap a raw token into these.
var (
	// ErrUnauthenticated is a missing, expired or otherwise invalid token.
	ErrUnauthenticated = errors.New("unauthenticated")
	// ErrUnavailable means the IdP/JWKS could not be reached (transient).
	ErrUnavailable = errors.New("identity provider unavailable")
)
