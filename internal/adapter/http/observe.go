package httpserver

import (
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/leosanner/desafio-jungle-go/internal/adapter/auth"
	"github.com/leosanner/desafio-jungle-go/internal/app"
)

const maxCorrelationID = 128

type scrapeHandler interface {
	Handler() http.Handler
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (s *Server) observe(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		id := ensureCorrelationID(r)
		w.Header().Set(correlationHeader, id)
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		if skipInstrumentation(r.URL.Path) {
			return
		}
		pattern := r.Pattern
		if pattern == "" {
			pattern = "unmatched"
		}
		s.metrics.ObserveHTTP(r.Method, pattern, sw.status, time.Since(start))
		attrs := []any{
			"correlationId", id,
			"method", r.Method,
			"pattern", pattern,
			"status", sw.status,
			"durationMs", time.Since(start).Milliseconds(),
		}
		if actor, ok := auth.ActorFrom(r.Context()); ok && actor.ProviderID != "" {
			attrs = append(attrs, "providerId", actor.ProviderID)
		}
		s.log.Info("http request", attrs...)
	})
}

func skipInstrumentation(path string) bool {
	return path == "/health/live" || path == "/health/ready" || path == "/metrics"
}

func ensureCorrelationID(r *http.Request) string {
	raw := strings.TrimSpace(r.Header.Get(correlationHeader))
	if validCorrelationID(raw) {
		return raw
	}
	id := app.UUIDGenerator{}.NewID()
	r.Header.Set(correlationHeader, id)
	return id
}

func validCorrelationID(s string) bool {
	if s == "" || utf8.RuneCountInString(s) > maxCorrelationID {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x21 || c > 0x7e || c == '"' || c == '\\' {
			return false
		}
	}
	return true
}
