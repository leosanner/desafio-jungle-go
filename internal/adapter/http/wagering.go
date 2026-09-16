package httpserver

import (
	"net/http"
	"time"

	"github.com/leosanner/desafio-jungle-go/internal/adapter/auth"
	"github.com/leosanner/desafio-jungle-go/internal/app"
	"github.com/leosanner/desafio-jungle-go/internal/domain"
)

type wageringRequest struct {
	ProviderID          string       `json:"providerId"`
	ExternalID          string       `json:"externalTransactionId"`
	PlayerID            string       `json:"playerId"`
	WalletID            string       `json:"walletId"`
	RoundID             string       `json:"roundId"`
	GameID              string       `json:"gameId"`
	Kind                string       `json:"kind"`
	Money               domain.Money `json:"money"`
	ReferenceExternalID string       `json:"referenceExternalTransactionId"`
}

type operationResponse struct {
	TransactionID    string        `json:"transactionId"`
	Status           string        `json:"status"`
	Balance          *domain.Money `json:"balance,omitempty"`
	FailureCode      string        `json:"failureCode,omitempty"`
	IdempotentReplay bool          `json:"idempotentReplay"`
}

type transactionResponse struct {
	TransactionID       string        `json:"transactionId"`
	ProviderID          string        `json:"providerId,omitempty"`
	ExternalID          string        `json:"externalTransactionId,omitempty"`
	PlayerID            string        `json:"playerId"`
	WalletID            string        `json:"walletId"`
	RoundID             string        `json:"roundId,omitempty"`
	GameID              string        `json:"gameId,omitempty"`
	Kind                string        `json:"kind"`
	Money               domain.Money  `json:"money"`
	ReferenceExternalID string        `json:"referenceExternalTransactionId,omitempty"`
	Status              string        `json:"status"`
	FailureCode         string        `json:"failureCode,omitempty"`
	Balance             *domain.Money `json:"balance"`
	CreatedAt           string        `json:"createdAt"`
	UpdatedAt           string        `json:"updatedAt"`
}

func (s *Server) handlePostWagering(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorOrUnauthorized(w, r, s)
	if !ok {
		return
	}
	if actor.Internal || actor.ProviderID == "" {
		s.writeError(w, http.StatusForbidden, "forbidden", "insufficient permissions")
		return
	}

	var req wageringRequest
	if err := decodeJSON(r, &req); err != nil {
		malformedOrDomain(w, s, err)
		return
	}
	if req.ProviderID == "" {
		req.ProviderID = actor.ProviderID
	}
	if req.ProviderID != actor.ProviderID {
		s.writeError(w, http.StatusForbidden, "forbidden", "insufficient permissions")
		return
	}

	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		s.writeFailure(w, http.StatusBadRequest, "invalid", "idempotency key is required", domain.FailureMissingIdentity)
		return
	}

	kind, err := domain.ParseKind(req.Kind)
	if err != nil {
		s.writeUseCaseError(w, err)
		return
	}

	out, err := s.svc.Submit(r.Context(), app.SubmitCommand{
		Actor:               actor,
		IdempotencyKey:      key,
		ProviderID:          req.ProviderID,
		ExternalID:          req.ExternalID,
		PlayerID:            req.PlayerID,
		WalletID:            req.WalletID,
		RoundID:             req.RoundID,
		GameID:              req.GameID,
		Kind:                kind,
		Money:               req.Money,
		ReferenceExternalID: req.ReferenceExternalID,
		CorrelationID:       correlationID(r),
	})
	if err != nil {
		s.writeUseCaseError(w, err)
		return
	}
	s.writeOperation(w, out)
}

func (s *Server) writeOperation(w http.ResponseWriter, out app.SubmitResult) {
	tx := out.Transaction
	body := operationResponse{
		TransactionID:    tx.ID(),
		Status:           string(tx.Status()),
		FailureCode:      string(tx.FailureCode()),
		IdempotentReplay: out.IdempotentReplay,
	}
	if bal, ok := tx.ResultBalance(); ok {
		body.Balance = &bal
	}
	status := http.StatusOK
	switch tx.Status() {
	case domain.StatusRejected:
		status = http.StatusUnprocessableEntity
	case domain.StatusPendingReference:
		status = http.StatusAccepted
	}
	s.writeJSON(w, status, body)
}

func (s *Server) handleGetTransaction(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorOrUnauthorized(w, r, s)
	if !ok {
		return
	}
	tx, err := s.svc.GetTransaction(r.Context(), actor, r.PathValue("transactionId"))
	if err != nil {
		s.writeUseCaseError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, transactionJSON(tx))
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
	tx, err := s.svc.GetByProviderExternalID(r.Context(), actor, providerID, r.PathValue("externalTransactionId"))
	if err != nil {
		s.writeUseCaseError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, transactionJSON(tx))
}

func transactionJSON(tx domain.WagerTransaction) transactionResponse {
	bal, ok := tx.ResultBalance()
	return transactionResponse{
		TransactionID:       tx.ID(),
		ProviderID:          tx.ProviderID(),
		ExternalID:          tx.ExternalID(),
		PlayerID:            tx.PlayerID(),
		WalletID:            tx.WalletID(),
		RoundID:             tx.RoundID(),
		GameID:              tx.GameID(),
		Kind:                string(tx.Kind()),
		Money:               tx.Money(),
		ReferenceExternalID: tx.ReferenceExternalID(),
		Status:              string(tx.Status()),
		FailureCode:         string(tx.FailureCode()),
		Balance:             moneyPtr(bal, ok),
		CreatedAt:           tx.CreatedAt().UTC().Format(time.RFC3339Nano),
		UpdatedAt:           tx.UpdatedAt().UTC().Format(time.RFC3339Nano),
	}
}
