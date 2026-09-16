package httpserver

import (
	"net/http"
	"time"

	"github.com/leosanner/desafio-jungle-go/internal/app"
	"github.com/leosanner/desafio-jungle-go/internal/domain"
)

type openWalletRequest struct {
	PlayerID       string       `json:"playerId"`
	InitialBalance domain.Money `json:"initialBalance"`
}

type walletResponse struct {
	ID       string       `json:"id"`
	PlayerID string       `json:"playerId"`
	Balance  domain.Money `json:"balance"`
	Version  int64        `json:"version"`
}

func walletJSON(w domain.Wallet) walletResponse {
	return walletResponse{
		ID:       w.ID(),
		PlayerID: w.PlayerID(),
		Balance:  w.Balance(),
		Version:  w.Version(),
	}
}

func (s *Server) handleOpenWallet(w http.ResponseWriter, r *http.Request) {
	var req openWalletRequest
	if err := decodeJSON(r, &req); err != nil {
		malformedOrDomain(w, s, err)
		return
	}
	if err := requireID("playerId", req.PlayerID); err != nil {
		s.writeUseCaseError(w, err)
		return
	}
	out, err := s.svc.OpenWallet(r.Context(), app.OpenWalletCommand{
		PlayerID:       req.PlayerID,
		InitialBalance: req.InitialBalance,
		CorrelationID:  correlationID(r),
	})
	if err != nil {
		s.writeUseCaseError(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, walletJSON(out.Wallet))
}

func (s *Server) handleGetWallet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("walletId")
	wallet, err := s.svc.GetWallet(r.Context(), id)
	if err != nil {
		s.writeUseCaseError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, walletJSON(wallet))
}

type ledgerItem struct {
	ID            string       `json:"id"`
	TransactionID string       `json:"transactionId"`
	Direction     string       `json:"direction"`
	Money         domain.Money `json:"money"`
	BalanceBefore domain.Money `json:"balanceBefore"`
	BalanceAfter  domain.Money `json:"balanceAfter"`
	CreatedAt     string       `json:"createdAt"`
}

type ledgerPageResponse struct {
	Items      []ledgerItem `json:"items"`
	NextCursor string       `json:"nextCursor,omitempty"`
}

func (s *Server) handleListLedger(w http.ResponseWriter, r *http.Request) {
	limit, err := app.ParseLimit(r.URL.Query().Get("limit"))
	if err != nil {
		s.writeUseCaseError(w, err)
		return
	}
	page, err := s.svc.ListLedger(r.Context(), r.PathValue("walletId"), r.URL.Query().Get("cursor"), limit)
	if err != nil {
		s.writeUseCaseError(w, err)
		return
	}
	items := make([]ledgerItem, 0, len(page.Entries))
	for _, e := range page.Entries {
		items = append(items, ledgerItem{
			ID:            e.ID(),
			TransactionID: e.TransactionID(),
			Direction:     string(e.Direction()),
			Money:         e.Money(),
			BalanceBefore: e.BalanceBefore(),
			BalanceAfter:  e.BalanceAfter(),
			CreatedAt:     e.CreatedAt().UTC().Format(time.RFC3339Nano),
		})
	}
	s.writeJSON(w, http.StatusOK, ledgerPageResponse{Items: items, NextCursor: page.NextCursor})
}

type reconciliationResponse struct {
	WalletID          string       `json:"walletId"`
	StoredBalance     domain.Money `json:"storedBalance"`
	CalculatedBalance domain.Money `json:"calculatedBalance"`
	Difference        domain.Money `json:"difference"`
	Consistent        bool         `json:"consistent"`
	CheckedEntries    int          `json:"checkedEntries"`
}

func (s *Server) handleReconcile(w http.ResponseWriter, r *http.Request) {
	rec, err := s.svc.ReconcileWallet(r.Context(), r.PathValue("walletId"))
	if err != nil {
		s.writeUseCaseError(w, err)
		return
	}
	if !rec.Consistent {
		s.log.Warn("wallet reconciliation diverged",
			"walletId", rec.WalletID,
			"checkedEntries", rec.CheckedEntries,
		)
	}
	s.writeJSON(w, http.StatusOK, reconciliationResponse{
		WalletID:          rec.WalletID,
		StoredBalance:     rec.StoredBalance,
		CalculatedBalance: rec.CalculatedBalance,
		Difference:        rec.Difference,
		Consistent:        rec.Consistent,
		CheckedEntries:    rec.CheckedEntries,
	})
}
