package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
)

// Checker is a named dependency probed by GET /health/ready.
type Checker interface {
	Name() string
	Check(ctx context.Context) error
}

// Checkers is the set of readiness probes. Provided as a single Fx value
// so two Checker implementations are not ambiguous.
type Checkers []Checker

type statusResponse struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks,omitempty"`
}

func (s *Server) handleLive(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, statusResponse{Status: "ok"})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if s.shuttingDown.Load() {
		s.writeJSON(w, http.StatusServiceUnavailable, statusResponse{
			Status: "unavailable",
			Checks: map[string]string{"shutdown": "in progress"},
		})
		return
	}

	ctx := r.Context()
	checks := make(map[string]string, len(s.checkers))
	ok := true
	for _, c := range s.checkers {
		if err := c.Check(ctx); err != nil {
			ok = false
			checks[c.Name()] = err.Error()
			continue
		}
		checks[c.Name()] = "ok"
	}
	if !ok {
		s.writeJSON(w, http.StatusServiceUnavailable, statusResponse{
			Status: "unavailable",
			Checks: checks,
		})
		return
	}
	s.writeJSON(w, http.StatusOK, statusResponse{Status: "ok"})
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, body statusResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		s.log.Error("health: write json", "err", err)
	}
}
