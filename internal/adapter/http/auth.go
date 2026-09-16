package httpserver

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/leosanner/desafio-jungle-go/internal/adapter/auth"
	"github.com/leosanner/desafio-jungle-go/internal/app"
)

const maxJSONBody = 1 << 20

type errorBody struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, ok := bearerToken(r.Header.Get("Authorization"))
		if !ok {
			setWWWAuthenticate(w, "")
			s.writeError(w, http.StatusUnauthorized, "unauthenticated", "missing or invalid bearer token")
			return
		}
		actor, err := s.tokens.Verify(r.Context(), raw)
		if err != nil {
			if errors.Is(err, auth.ErrUnavailable) {
				s.writeError(w, http.StatusServiceUnavailable, "unavailable", "identity provider unavailable")
				return
			}
			s.log.Info("auth rejected", "reason", classifyAuthReason(err))
			setWWWAuthenticate(w, "invalid_token")
			s.writeError(w, http.StatusUnauthorized, "unauthenticated", "missing or invalid bearer token")
			return
		}
		next.ServeHTTP(w, r.WithContext(auth.WithActor(r.Context(), actor)))
	})
}

func setWWWAuthenticate(w http.ResponseWriter, errCode string) {
	v := `Bearer realm="wagering"`
	if errCode != "" {
		v += `, error="` + errCode + `"`
	}
	w.Header().Set("WWW-Authenticate", v)
}

func (s *Server) requireInternal(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actor, ok := auth.ActorFrom(r.Context())
		if !ok || !actor.Internal {
			s.writeError(w, http.StatusForbidden, "forbidden", "insufficient permissions")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handlePostWagering(w http.ResponseWriter, r *http.Request) {
	actor, ok := auth.ActorFrom(r.Context())
	if !ok || actor.Internal || actor.ProviderID == "" {
		s.writeError(w, http.StatusForbidden, "forbidden", "insufficient permissions")
		return
	}
	if err := requireMatchingProviderID(r, actor); err != nil {
		if errors.Is(err, errMalformedJSON) {
			s.writeError(w, http.StatusBadRequest, "invalid", "malformed json")
			return
		}
		s.writeError(w, http.StatusForbidden, "forbidden", "insufficient permissions")
		return
	}
	s.notImplemented(w, r)
}

func (s *Server) handleProviderTransaction(w http.ResponseWriter, r *http.Request) {
	actor, ok := auth.ActorFrom(r.Context())
	if !ok {
		s.writeError(w, http.StatusUnauthorized, "unauthenticated", "missing or invalid bearer token")
		return
	}
	providerID := r.PathValue("providerId")
	if !actor.CanAccessProvider(providerID) {
		s.writeError(w, http.StatusForbidden, "forbidden", "insufficient permissions")
		return
	}
	s.notImplemented(w, r)
}

func (s *Server) notImplemented(w http.ResponseWriter, _ *http.Request) {
	s.writeError(w, http.StatusNotImplemented, "not_implemented", "handler not implemented")
}

func (s *Server) writeError(w http.ResponseWriter, status int, code, message string) {
	s.writeJSON(w, status, errorBody{Error: code, Message: message})
}

func bearerToken(header string) (string, bool) {
	const prefix = "bearer "
	if header == "" {
		return "", false
	}
	if len(header) < len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return "", false
	}
	tok := strings.TrimSpace(header[len(prefix):])
	if tok == "" {
		return "", false
	}
	return tok, true
}

func classifyAuthReason(err error) string {
	if errors.Is(err, auth.ErrUnavailable) {
		return "unavailable"
	}
	msg := err.Error()
	if strings.Contains(msg, "expired") {
		return "expired"
	}
	return "invalid"
}

var errMalformedJSON = errors.New("malformed json")
var errProviderMismatch = errors.New("provider mismatch")

func requireMatchingProviderID(r *http.Request, actor app.Actor) error {
	if r.Body == nil {
		return nil
	}
	defer r.Body.Close()
	limited := http.MaxBytesReader(nil, r.Body, maxJSONBody)
	var body struct {
		ProviderID string `json:"providerId"`
	}
	dec := json.NewDecoder(limited)
	if err := dec.Decode(&body); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return errMalformedJSON
	}
	if body.ProviderID != "" && body.ProviderID != actor.ProviderID {
		return errProviderMismatch
	}
	return nil
}
