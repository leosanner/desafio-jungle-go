package httpserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/leosanner/desafio-jungle-go/internal/adapter/auth"
	"github.com/leosanner/desafio-jungle-go/internal/app"
	"github.com/leosanner/desafio-jungle-go/internal/domain"
)

const maxJSONBody = 1 << 20
const correlationHeader = "X-Correlation-Id"

type errorBody struct {
	Error       string `json:"error"`
	Message     string `json:"message"`
	FailureCode string `json:"failureCode,omitempty"`
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

func (s *Server) writeError(w http.ResponseWriter, status int, code, message string) {
	s.writeJSON(w, status, errorBody{Error: code, Message: message})
}

func (s *Server) writeFailure(w http.ResponseWriter, status int, code, message string, failure domain.FailureCode) {
	s.writeJSON(w, status, errorBody{Error: code, Message: message, FailureCode: string(failure)})
}

func (s *Server) writeUseCaseError(w http.ResponseWriter, err error) {
	if err == nil {
		return
	}
	if errors.Is(err, app.ErrForbidden) {
		s.writeError(w, http.StatusForbidden, "forbidden", "insufficient permissions")
		return
	}
	if errors.Is(err, app.ErrNotFound) {
		s.writeError(w, http.StatusNotFound, "not_found", "resource not found")
		return
	}
	if errors.Is(err, app.ErrUnavailable) {
		s.writeError(w, http.StatusServiceUnavailable, "unavailable", "service temporarily unavailable")
		return
	}
	if errors.Is(err, app.ErrConflict) {
		s.writeError(w, http.StatusConflict, "conflict", "resource already exists")
		return
	}
	code, class, ok := domain.Classify(err)
	if ok {
		switch class {
		case domain.FailureClassValidation:
			s.writeFailure(w, http.StatusBadRequest, "invalid", err.Error(), code)
			return
		case domain.FailureClassRejection:
			switch code {
			case domain.FailureIdempotencyPayloadConflict, domain.FailureDuplicateExternalTransaction:
				s.writeFailure(w, http.StatusConflict, "conflict", err.Error(), code)
				return
			default:
				s.writeFailure(w, http.StatusUnprocessableEntity, "rejected", err.Error(), code)
				return
			}
		}
	}
	s.log.Error("http: unhandled use case error", "err", err)
	s.writeError(w, http.StatusServiceUnavailable, "unavailable", "service temporarily unavailable")
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

func decodeJSON(r *http.Request, dst any) error {
	if r.Body == nil {
		return errMalformedJSON
	}
	defer r.Body.Close()
	limited := http.MaxBytesReader(nil, r.Body, maxJSONBody)
	dec := json.NewDecoder(limited)
	if err := dec.Decode(dst); err != nil {
		var ce *domain.ClassifiedError
		if errors.As(err, &ce) {
			return err
		}
		return errMalformedJSON
	}
	return nil
}

func correlationID(r *http.Request) string {
	return strings.TrimSpace(r.Header.Get(correlationHeader))
}

func actorOrUnauthorized(w http.ResponseWriter, r *http.Request, s *Server) (app.Actor, bool) {
	actor, ok := auth.ActorFrom(r.Context())
	if !ok {
		s.writeError(w, http.StatusUnauthorized, "unauthenticated", "missing or invalid bearer token")
		return app.Actor{}, false
	}
	return actor, true
}

var errMalformedJSON = errors.New("malformed json")

func malformedOrDomain(w http.ResponseWriter, s *Server, err error) {
	if errors.Is(err, errMalformedJSON) {
		s.writeError(w, http.StatusBadRequest, "invalid", "malformed json")
		return
	}
	s.writeUseCaseError(w, err)
}

func moneyPtr(m domain.Money, ok bool) *domain.Money {
	if !ok {
		return nil
	}
	return &m
}

func requireID(name, value string) error {
	if strings.TrimSpace(value) == "" {
		return domain.NewValidation(domain.FailureMissingIdentity, fmt.Errorf("%w: %s", domain.ErrMissingIdentity, name))
	}
	return nil
}
