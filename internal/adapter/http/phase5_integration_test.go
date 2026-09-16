//go:build integration

package httpserver

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/leosanner/desafio-jungle-go/internal/adapter/postgres"
	"github.com/leosanner/desafio-jungle-go/internal/app"
	"github.com/leosanner/desafio-jungle-go/internal/config"
)

func TestHTTPPhase5UseCases(t *testing.T) {
	t.Parallel()
	env := newHTTPEnv(t)
	internal := env.server(t, app.Actor{ClientID: "wagering-internal", Internal: true})
	provider := env.server(t, app.Actor{ClientID: "provider-a", ProviderID: "provider-a"})
	other := env.server(t, app.Actor{ClientID: "provider-b", ProviderID: "provider-b"})

	playerID := uniqueID(t, "player-")
	openRec := doJSON(t, internal, http.MethodPost, "/wallets", "", map[string]any{
		"playerId":       playerID,
		"initialBalance": map[string]string{"amount": "100.00", "currency": "BRL"},
	})
	if openRec.Code != http.StatusCreated {
		t.Fatalf("open status = %d body %s", openRec.Code, openRec.Body.String())
	}
	var wallet walletResponse
	decodeBody(t, openRec, &wallet)
	if wallet.Version != 1 || wallet.Balance.AmountString() != "100.00" {
		t.Fatalf("wallet = %+v", wallet)
	}
	if env.countOutbox(t) != 2 {
		t.Fatalf("opening outbox = %d, want 2", env.countOutbox(t))
	}

	getRec := doJSON(t, internal, http.MethodGet, "/wallets/"+wallet.ID, "", nil)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get wallet = %d", getRec.Code)
	}

	ledRec := doJSON(t, internal, http.MethodGet, "/wallets/"+wallet.ID+"/ledger?limit=50", "", nil)
	if ledRec.Code != http.StatusOK {
		t.Fatalf("ledger = %d", ledRec.Code)
	}

	betBody := map[string]any{
		"providerId":            "provider-a",
		"externalTransactionId": "bet-1",
		"playerId":              playerID,
		"walletId":              wallet.ID,
		"roundId":               "round-1",
		"gameId":                "fortune-chimp",
		"kind":                  "BET",
		"money":                 map[string]string{"amount": "25.00", "currency": "BRL"},
	}
	betRec := doJSON(t, provider, http.MethodPost, "/wagering/transactions", "provider-a:bet-1", betBody)
	if betRec.Code != http.StatusOK {
		t.Fatalf("bet = %d %s", betRec.Code, betRec.Body.String())
	}
	var betOp operationResponse
	decodeBody(t, betRec, &betOp)
	if betOp.IdempotentReplay || betOp.Status != "PROCESSED" {
		t.Fatalf("bet %+v", betOp)
	}
	if betOp.Balance == nil || betOp.Balance.AmountString() != "75.00" {
		t.Fatalf("balance %+v", betOp.Balance)
	}

	replay := doJSON(t, provider, http.MethodPost, "/wagering/transactions", "provider-a:bet-1", betBody)
	if replay.Code != http.StatusOK {
		t.Fatalf("replay = %d %s", replay.Code, replay.Body.String())
	}
	var replayOp operationResponse
	decodeBody(t, replay, &replayOp)
	if !replayOp.IdempotentReplay || replayOp.Balance.AmountString() != "75.00" {
		t.Fatalf("replay %+v", replayOp)
	}

	conflictBody := cloneMap(betBody)
	conflictBody["money"] = map[string]string{"amount": "26.00", "currency": "BRL"}
	conf := doJSON(t, provider, http.MethodPost, "/wagering/transactions", "provider-a:bet-1", conflictBody)
	if conf.Code != http.StatusConflict {
		t.Fatalf("payload conflict = %d", conf.Code)
	}

	dupExt := cloneMap(betBody)
	dup := doJSON(t, provider, http.MethodPost, "/wagering/transactions", "other-key", dupExt)
	if dup.Code != http.StatusConflict {
		t.Fatalf("dup external = %d %s", dup.Code, dup.Body.String())
	}

	// Move wallet further then replay original snapshot.
	bet2 := cloneMap(betBody)
	bet2["externalTransactionId"] = "bet-2"
	bet2Rec := doJSON(t, provider, http.MethodPost, "/wagering/transactions", "provider-a:bet-2", bet2)
	if bet2Rec.Code != http.StatusOK {
		t.Fatalf("bet2 = %d %s", bet2Rec.Code, bet2Rec.Body.String())
	}
	replay2 := doJSON(t, provider, http.MethodPost, "/wagering/transactions", "provider-a:bet-1", betBody)
	var replay2Op operationResponse
	decodeBody(t, replay2, &replay2Op)
	if replay2Op.Balance.AmountString() != "75.00" {
		t.Fatalf("original snapshot lost: %s", replay2Op.Balance.AmountString())
	}

	txRec := doJSON(t, provider, http.MethodGet, "/wagering/transactions/"+betOp.TransactionID, "", nil)
	if txRec.Code != http.StatusOK {
		t.Fatalf("get tx = %d", txRec.Code)
	}
	hidden := doJSON(t, other, http.MethodGet, "/wagering/transactions/"+betOp.TransactionID, "", nil)
	if hidden.Code != http.StatusNotFound {
		t.Fatalf("other provider = %d, want 404", hidden.Code)
	}

	extRec := doJSON(t, provider, http.MethodGet, "/providers/provider-a/wagering/transactions/bet-1", "", nil)
	if extRec.Code != http.StatusOK {
		t.Fatalf("get by external = %d", extRec.Code)
	}

	recon := doJSON(t, internal, http.MethodPost, "/wallets/"+wallet.ID+"/reconciliation", "", nil)
	if recon.Code != http.StatusOK {
		t.Fatalf("recon = %d %s", recon.Code, recon.Body.String())
	}
	var rec reconciliationResponse
	decodeBody(t, recon, &rec)
	if !rec.Consistent {
		t.Fatalf("inconsistent %+v", rec)
	}

	zeroPlayer := uniqueID(t, "player-")
	zeroRec := doJSON(t, internal, http.MethodPost, "/wallets", "", map[string]any{
		"playerId":       zeroPlayer,
		"initialBalance": map[string]string{"amount": "0.00", "currency": "BRL"},
	})
	if zeroRec.Code != http.StatusCreated {
		t.Fatalf("zero open = %d %s", zeroRec.Code, zeroRec.Body.String())
	}
}

func TestHTTPConcurrentSameBet(t *testing.T) {
	t.Parallel()
	env := newHTTPEnv(t)
	internal := env.server(t, app.Actor{Internal: true, ClientID: "wagering-internal"})
	provider := env.server(t, app.Actor{ProviderID: "provider-a", ClientID: "provider-a"})
	playerID := uniqueID(t, "player-")
	openRec := doJSON(t, internal, http.MethodPost, "/wallets", "", map[string]any{
		"playerId":       playerID,
		"initialBalance": map[string]string{"amount": "100.00", "currency": "BRL"},
	})
	var wallet walletResponse
	decodeBody(t, openRec, &wallet)

	body := map[string]any{
		"providerId":            "provider-a",
		"externalTransactionId": "same-bet",
		"playerId":              playerID,
		"walletId":              wallet.ID,
		"roundId":               "round-1",
		"gameId":                "game-1",
		"kind":                  "BET",
		"money":                 map[string]string{"amount": "10.00", "currency": "BRL"},
	}

	const n = 50
	start := make(chan struct{})
	var wg sync.WaitGroup
	var processed atomic.Int64
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			rec := doJSON(t, provider, http.MethodPost, "/wagering/transactions", "provider-a:same-bet", body)
			if rec.Code != http.StatusOK {
				t.Errorf("status %d %s", rec.Code, rec.Body.String())
				return
			}
			var op operationResponse
			decodeBody(t, rec, &op)
			if !op.IdempotentReplay {
				processed.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	if processed.Load() != 1 {
		t.Fatalf("first-time processed = %d, want 1", processed.Load())
	}
	got := doJSON(t, internal, http.MethodGet, "/wallets/"+wallet.ID, "", nil)
	var w walletResponse
	decodeBody(t, got, &w)
	if w.Balance.AmountString() != "90.00" {
		t.Fatalf("balance = %s", w.Balance.AmountString())
	}
}

func TestHTTPTwoBetsOnHundred(t *testing.T) {
	t.Parallel()
	env := newHTTPEnv(t)
	internal := env.server(t, app.Actor{Internal: true, ClientID: "wagering-internal"})
	provider := env.server(t, app.Actor{ProviderID: "provider-a", ClientID: "provider-a"})
	playerID := uniqueID(t, "player-")
	openRec := doJSON(t, internal, http.MethodPost, "/wallets", "", map[string]any{
		"playerId":       playerID,
		"initialBalance": map[string]string{"amount": "100.00", "currency": "BRL"},
	})
	var wallet walletResponse
	decodeBody(t, openRec, &wallet)

	start := make(chan struct{})
	var wg sync.WaitGroup
	results := make([]int, 2)
	for i, ext := range []string{"bet-a", "bet-b"} {
		wg.Add(1)
		go func(i int, ext string) {
			defer wg.Done()
			<-start
			body := map[string]any{
				"providerId":            "provider-a",
				"externalTransactionId": ext,
				"playerId":              playerID,
				"walletId":              wallet.ID,
				"roundId":               "round-1",
				"gameId":                "game-1",
				"kind":                  "BET",
				"money":                 map[string]string{"amount": "80.00", "currency": "BRL"},
			}
			rec := doJSON(t, provider, http.MethodPost, "/wagering/transactions", "provider-a:"+ext, body)
			results[i] = rec.Code
		}(i, ext)
	}
	close(start)
	wg.Wait()

	var ok, rejected int
	for _, code := range results {
		switch code {
		case http.StatusOK:
			ok++
		case http.StatusUnprocessableEntity:
			rejected++
		default:
			t.Fatalf("unexpected status %d", code)
		}
	}
	if ok != 1 || rejected != 1 {
		t.Fatalf("ok=%d rejected=%d results=%v", ok, rejected, results)
	}
	got := doJSON(t, internal, http.MethodGet, "/wallets/"+wallet.ID, "", nil)
	var w walletResponse
	decodeBody(t, got, &w)
	if w.Balance.AmountString() != "20.00" {
		t.Fatalf("balance = %s", w.Balance.AmountString())
	}
}

func TestHTTPDistinctWalletsParallel(t *testing.T) {
	t.Parallel()
	env := newHTTPEnv(t)
	internal := env.server(t, app.Actor{Internal: true, ClientID: "wagering-internal"})
	provider := env.server(t, app.Actor{ProviderID: "provider-a", ClientID: "provider-a"})

	type opened struct {
		player string
		id     string
	}
	wallets := make([]opened, 2)
	for i := range wallets {
		player := uniqueID(t, "player-")
		rec := doJSON(t, internal, http.MethodPost, "/wallets", "", map[string]any{
			"playerId":       player,
			"initialBalance": map[string]string{"amount": "50.00", "currency": "BRL"},
		})
		var w walletResponse
		decodeBody(t, rec, &w)
		wallets[i] = opened{player: player, id: w.ID}
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, w := range wallets {
		wg.Add(1)
		go func(i int, w opened) {
			defer wg.Done()
			<-start
			ext := fmt.Sprintf("par-%d", i)
			body := map[string]any{
				"providerId":            "provider-a",
				"externalTransactionId": ext,
				"playerId":              w.player,
				"walletId":              w.id,
				"roundId":               "round-1",
				"gameId":                "game-1",
				"kind":                  "BET",
				"money":                 map[string]string{"amount": "10.00", "currency": "BRL"},
			}
			rec := doJSON(t, provider, http.MethodPost, "/wagering/transactions", "provider-a:"+ext, body)
			if rec.Code != http.StatusOK {
				t.Errorf("wallet %d status %d %s", i, rec.Code, rec.Body.String())
			}
		}(i, w)
	}
	close(start)
	wg.Wait()
}

type httpEnv struct {
	dsn string
	uow app.UnitOfWork
}

func newHTTPEnv(t *testing.T) *httpEnv {
	t.Helper()
	dsn := createIsolatedDatabase(t)
	path := migrationsDir(t)
	if err := postgres.RunUp(path, migrateDSN(dsn)); err != nil {
		t.Fatalf("RunUp: %v", err)
	}
	pool, err := postgres.NewPool(config.Config{PostgresDSN: dsn})
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return &httpEnv{dsn: dsn, uow: postgres.NewUnitOfWork(pool)}
}

func (e *httpEnv) server(t *testing.T, actor app.Actor) *Server {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := app.NewService(e.uow, app.SystemClock{}, app.UUIDGenerator{}, app.NopMetrics{})
	return New(config.Config{HTTPAddr: ":0"}, log, nil, staticTokens{actor: actor}, svc)
}

func (e *httpEnv) countOutbox(t *testing.T) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, e.dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	var n int
	if err := conn.QueryRow(ctx, `SELECT COUNT(*) FROM wagering.outbox_events`).Scan(&n); err != nil {
		t.Fatalf("count outbox: %v", err)
	}
	return n
}

func doJSON(t *testing.T, s *Server, method, path, idem string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rdr = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, rdr)
	req.Header.Set("Authorization", "Bearer t")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if idem != "" {
		req.Header.Set("Idempotency-Key", idem)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder, dst any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), dst); err != nil {
		t.Fatalf("json %v body %s", err, rec.Body.String())
	}
}

func cloneMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func uniqueID(t *testing.T, prefix string) string {
	t.Helper()
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatal(err)
	}
	return prefix + hex.EncodeToString(b[:])
}

func migrationsDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", "migrations"))
}

func migrateDSN(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		return dsn
	}
	q := u.Query()
	q.Set("x-migrations-table", `"public"."schema_migrations"`)
	q.Set("x-migrations-table-quoted", "true")
	u.RawQuery = q.Encode()
	return u.String()
}

func createIsolatedDatabase(t *testing.T) string {
	t.Helper()
	adminDSN := os.Getenv("POSTGRES_DSN")
	if adminDSN == "" {
		t.Skip("POSTGRES_DSN is not set")
	}
	dbName := "wagering_it_" + uniqueID(t, "")
	ident := pgx.Identifier{dbName}.Sanitize()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cfg, err := pgxpool.ParseConfig(adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	conn, err := pgx.ConnectConfig(ctx, cfg.ConnConfig)
	if err != nil {
		t.Fatalf("admin connect: %v", err)
	}
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+ident); err != nil {
		conn.Close(ctx)
		t.Fatal(err)
	}
	conn.Close(ctx)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		c, err := pgx.ConnectConfig(ctx, cfg.ConnConfig)
		if err != nil {
			return
		}
		defer c.Close(context.Background())
		_, _ = c.Exec(ctx, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()`, dbName)
		_, _ = c.Exec(ctx, "DROP DATABASE IF EXISTS "+ident+" WITH (FORCE)")
	})
	u, err := url.Parse(adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + dbName
	return u.String()
}
