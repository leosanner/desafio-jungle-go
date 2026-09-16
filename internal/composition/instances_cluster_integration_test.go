//go:build integration

package composition_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/leosanner/desafio-jungle-go/internal/adapter/postgres"
	sqsadapter "github.com/leosanner/desafio-jungle-go/internal/adapter/sqs"
	"github.com/leosanner/desafio-jungle-go/internal/config"
)

const instanceCount = 3

var (
	binOnce sync.Once
	binPath string
	binErr  error
)

func repoRoot() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "."
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func wageringBinary(t *testing.T) string {
	t.Helper()
	binOnce.Do(func() {
		dir, err := os.MkdirTemp("", "wagering-it-bin-")
		if err != nil {
			binErr = err
			return
		}
		binPath = filepath.Join(dir, "wagering")
		cmd := exec.Command("go", "build", "-o", binPath, "./cmd/wagering")
		cmd.Dir = repoRoot()
		out, err := cmd.CombinedOutput()
		if err != nil {
			binErr = fmt.Errorf("go build: %w\n%s", err, out)
		}
	})
	if binErr != nil {
		t.Fatal(binErr)
	}
	return binPath
}

func requireClusterEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"POSTGRES_DSN",
		"OIDC_ISSUER",
		"AWS_ENDPOINT_URL",
		"AWS_REGION",
		"AWS_ACCESS_KEY_ID",
		"AWS_SECRET_ACCESS_KEY",
	} {
		if strings.TrimSpace(os.Getenv(key)) == "" {
			t.Skip(key + " is not set")
		}
	}
	waitDiscovery(t, strings.TrimRight(os.Getenv("OIDC_ISSUER"), "/"))
}

func getenvDefault(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func uniqueSuffix(t *testing.T) string {
	t.Helper()
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b[:])
}

func uniqueID(t *testing.T, prefix string) string {
	t.Helper()
	return prefix + uniqueSuffix(t)
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
	dbName := "wagering_it_" + uniqueSuffix(t)
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

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}

func waitDiscovery(t *testing.T, issuer string) {
	t.Helper()
	u := issuer + "/.well-known/openid-configuration"
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(u)
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			return
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("oidc discovery not ready: %s", u)
}

func clientCredentials(t *testing.T, issuer, clientID, secret string) string {
	t.Helper()
	tokenURL := strings.TrimRight(issuer, "/") + "/protocol/openid-connect/token"
	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {clientID},
		"client_secret": {secret},
	}
	resp, err := http.PostForm(tokenURL, form)
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("token status %d: %s", resp.StatusCode, body)
	}
	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("token json: %v", err)
	}
	if out.AccessToken == "" {
		t.Fatal("empty access_token")
	}
	return out.AccessToken
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type instanceProc struct {
	cmd    *exec.Cmd
	base   string
	log    *lockedBuffer
	exited chan struct{}
	err    error
}

func (p *instanceProc) kill() {
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
	<-p.exited
}

func (p *instanceProc) stop() {
	select {
	case <-p.exited:
		return
	default:
	}
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Signal(syscall.SIGTERM)
	}
	select {
	case <-p.exited:
	case <-time.After(15 * time.Second):
		if p.cmd.Process != nil {
			_ = p.cmd.Process.Kill()
		}
		<-p.exited
	}
}

type instanceCluster struct {
	instances []*instanceProc
	dsn       string
	sqs       *sqsadapter.Client
	internal  string
	provider  string
	http      *http.Client
}

func childEnv(overrides map[string]string) []string {
	skip := make(map[string]struct{}, len(overrides))
	for k := range overrides {
		skip[k] = struct{}{}
	}
	out := make([]string, 0, 64)
	for _, kv := range os.Environ() {
		k, _, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if _, found := skip[k]; found {
			continue
		}
		out = append(out, kv)
	}
	for k, v := range overrides {
		out = append(out, k+"="+v)
	}
	return out
}

func startCluster(t *testing.T) *instanceCluster {
	t.Helper()
	requireClusterEnv(t)
	bin := wageringBinary(t)
	dsn := createIsolatedDatabase(t)
	migrations := filepath.Join(repoRoot(), "migrations")
	if err := postgres.RunUp(migrations, migrateDSN(dsn)); err != nil {
		t.Fatalf("RunUp: %v", err)
	}

	suffix := uniqueSuffix(t)
	wager := "wager-inst-" + suffix + ".fifo"
	dlq := "wager-inst-dlq-" + suffix + ".fifo"
	events := "wager-inst-events-" + suffix + ".fifo"
	sqsCfg := config.Config{
		AWSRegion:          getenvDefault("AWS_REGION", "us-east-1"),
		AWSAccessKeyID:     os.Getenv("AWS_ACCESS_KEY_ID"),
		AWSSecretAccessKey: os.Getenv("AWS_SECRET_ACCESS_KEY"),
		AWSEndpointURL:     os.Getenv("AWS_ENDPOINT_URL"),
		SQSWagerQueueName:  wager,
		SQSWagerDLQName:    dlq,
		SQSEventsQueueName: events,
	}
	client, err := sqsadapter.NewClient(sqsCfg)
	if err != nil {
		t.Fatalf("sqs client: %v", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	if err := client.CreateFIFOQueueWithRedrive(ctx, wager, dlq, 5*time.Second, 5); err != nil {
		t.Fatalf("create inbound queues: %v", err)
	}
	if err := client.CreateFIFOQueue(ctx, events); err != nil {
		t.Fatalf("create events queue: %v", err)
	}
	if err := client.CheckQueues(ctx); err != nil {
		t.Fatalf("check queues: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		_ = client.DeleteQueue(bg, wager)
		_ = client.DeleteQueue(bg, dlq)
		_ = client.DeleteQueue(bg, events)
	})

	issuer := strings.TrimRight(os.Getenv("OIDC_ISSUER"), "/")
	cluster := &instanceCluster{
		dsn:      dsn,
		sqs:      client,
		internal: clientCredentials(t, issuer, "wagering-internal", "internal-secret"),
		provider: clientCredentials(t, issuer, "provider-a", "provider-a-secret"),
		http:     &http.Client{Timeout: 20 * time.Second},
	}

	procs := make([]*instanceProc, instanceCount)
	t.Cleanup(func() {
		for _, p := range procs {
			if p == nil {
				continue
			}
			p.stop()
			if t.Failed() {
				t.Logf("instance %s logs:\n%s", p.base, p.log.String())
			}
		}
	})
	for i := 0; i < instanceCount; i++ {
		addr := freeAddr(t)
		logBuf := &lockedBuffer{}
		cmd := exec.Command(bin)
		cmd.Stdout = logBuf
		cmd.Stderr = logBuf
		cmd.Env = childEnv(map[string]string{
			"LOG_LEVEL":                 "info",
			"HTTP_ADDR":                 addr,
			"POSTGRES_DSN":              dsn,
			"MIGRATIONS_PATH":           migrations,
			"AWS_REGION":                getenvDefault("AWS_REGION", "us-east-1"),
			"AWS_ACCESS_KEY_ID":         os.Getenv("AWS_ACCESS_KEY_ID"),
			"AWS_SECRET_ACCESS_KEY":     os.Getenv("AWS_SECRET_ACCESS_KEY"),
			"AWS_ENDPOINT_URL":          os.Getenv("AWS_ENDPOINT_URL"),
			"AWS_EC2_METADATA_DISABLED": "true",
			"SQS_WAGER_QUEUE_NAME":      wager,
			"SQS_WAGER_DLQ_NAME":        dlq,
			"SQS_EVENTS_QUEUE_NAME":     events,
			"FX_START_TIMEOUT":          "30s",
			"FX_STOP_TIMEOUT":           "15s",
			"OUTBOX_POLL_INTERVAL":      "200ms",
			"OUTBOX_BATCH_SIZE":         "10",
			"OUTBOX_LEASE":              "10s",
			"OUTBOX_BACKOFF_MAX":        "5s",
			"SQS_VISIBILITY_TIMEOUT":    "5s",
			"SQS_WAIT_TIME":             "1s",
			"SQS_BACKOFF_MAX":           "5s",
			"PENDING_POLL_INTERVAL":     "100ms",
			"PENDING_BATCH_SIZE":        "10",
			"PENDING_LEASE":             "2s",
			"PENDING_BACKOFF_MAX":       "5s",
			"PENDING_MAX_ATTEMPTS":      "32",
			"PENDING_TTL":               "5m",
			"OIDC_ISSUER":               issuer,
			"OIDC_AUDIENCE":             getenvDefault("OIDC_AUDIENCE", "wagering-api"),
			"OIDC_JWKS_URL":             os.Getenv("OIDC_JWKS_URL"),
			"OIDC_INTERNAL_CLIENT":      getenvDefault("OIDC_INTERNAL_CLIENT", "wagering-internal"),
		})
		if err := cmd.Start(); err != nil {
			t.Fatalf("start instance %d: %v", i, err)
		}
		proc := &instanceProc{
			cmd:    cmd,
			base:   "http://" + addr,
			log:    logBuf,
			exited: make(chan struct{}),
		}
		go func(p *instanceProc, command *exec.Cmd) {
			p.err = command.Wait()
			close(p.exited)
		}(proc, cmd)
		procs[i] = proc
		waitReady(t, proc)
	}
	cluster.instances = procs
	return cluster
}

func waitReady(t *testing.T, p *instanceProc) {
	t.Helper()
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-p.exited:
			t.Fatalf("instance %s exited before ready: %v\n%s", p.base, p.err, p.log.String())
		default:
		}
		resp, err := http.Get(p.base + "/health/ready")
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("instance %s not ready\n%s", p.base, p.log.String())
}

func (c *instanceCluster) inst(i int) *instanceProc {
	return c.instances[i%len(c.instances)]
}

type httpResult struct {
	Status int
	Body   []byte
}

func (c *instanceCluster) do(t *testing.T, inst *instanceProc, method, path, token, idem string, body any) httpResult {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(t.Context(), method, inst.base+path, rdr)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if idem != "" {
		req.Header.Set("Idempotency-Key", idem)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return httpResult{Status: resp.StatusCode, Body: raw}
}

func decodeJSON(t *testing.T, raw []byte, dst any) {
	t.Helper()
	if err := json.Unmarshal(raw, dst); err != nil {
		t.Fatalf("json %v body %s", err, raw)
	}
}

type walletJSON struct {
	ID      string `json:"id"`
	Balance struct {
		Amount   string `json:"amount"`
		Currency string `json:"currency"`
	} `json:"balance"`
}

type operationJSON struct {
	TransactionID    string `json:"transactionId"`
	Status           string `json:"status"`
	FailureCode      string `json:"failureCode"`
	IdempotentReplay bool   `json:"idempotentReplay"`
	Balance          *struct {
		Amount string `json:"amount"`
	} `json:"balance"`
}

type transactionJSON struct {
	TransactionID string `json:"transactionId"`
	Status        string `json:"status"`
	FailureCode   string `json:"failureCode"`
}

type reconJSON struct {
	Consistent bool `json:"consistent"`
}

func (c *instanceCluster) openWallet(t *testing.T, inst *instanceProc, playerID, amount string) walletJSON {
	t.Helper()
	res := c.do(t, inst, http.MethodPost, "/wallets", c.internal, "", map[string]any{
		"playerId":       playerID,
		"initialBalance": map[string]string{"amount": amount, "currency": "BRL"},
	})
	if res.Status != http.StatusCreated {
		t.Fatalf("open wallet status %d %s", res.Status, res.Body)
	}
	var w walletJSON
	decodeJSON(t, res.Body, &w)
	return w
}

func (c *instanceCluster) getWallet(t *testing.T, inst *instanceProc, id string) walletJSON {
	t.Helper()
	res := c.do(t, inst, http.MethodGet, "/wallets/"+id, c.internal, "", nil)
	if res.Status != http.StatusOK {
		t.Fatalf("get wallet status %d %s", res.Status, res.Body)
	}
	var w walletJSON
	decodeJSON(t, res.Body, &w)
	return w
}

func (c *instanceCluster) submitBet(t *testing.T, inst *instanceProc, playerID, walletID, ext, amount string) httpResult {
	t.Helper()
	return c.do(t, inst, http.MethodPost, "/wagering/transactions", c.provider, "provider-a:"+ext, map[string]any{
		"providerId":            "provider-a",
		"externalTransactionId": ext,
		"playerId":              playerID,
		"walletId":              walletID,
		"roundId":               "round-1",
		"gameId":                "game-1",
		"kind":                  "BET",
		"money":                 map[string]string{"amount": amount, "currency": "BRL"},
	})
}

func (c *instanceCluster) queryInt(t *testing.T, sql string, args ...any) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, c.dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	var n int
	if err := conn.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (c *instanceCluster) debitCount(t *testing.T, walletID string) int {
	t.Helper()
	return c.queryInt(t, `SELECT COUNT(*) FROM wagering.wallet_ledger_entries WHERE wallet_id = $1 AND direction = 'DEBIT'`, walletID)
}

func (c *instanceCluster) ledgerCount(t *testing.T, walletID string) int {
	t.Helper()
	return c.queryInt(t, `SELECT COUNT(*) FROM wagering.wallet_ledger_entries WHERE wallet_id = $1`, walletID)
}

func (c *instanceCluster) inboxCount(t *testing.T) int {
	t.Helper()
	return c.queryInt(t, `SELECT COUNT(*) FROM wagering.inbox_messages`)
}

func (c *instanceCluster) assertReconciled(t *testing.T, inst *instanceProc, walletID string) {
	t.Helper()
	res := c.do(t, inst, http.MethodPost, "/wallets/"+walletID+"/reconciliation", c.internal, "", nil)
	if res.Status != http.StatusOK {
		t.Fatalf("recon status %d %s", res.Status, res.Body)
	}
	var rec reconJSON
	decodeJSON(t, res.Body, &rec)
	if !rec.Consistent {
		t.Fatalf("wallet %s not consistent", walletID)
	}
}

func inboundEnvelope(messageID, provider, walletID, playerID, extID, key, amount string) []byte {
	body := map[string]any{
		"messageId":  messageID,
		"type":       sqsadapter.EnvelopeTypeWagerTransactionRequested,
		"occurredAt": "2026-09-08T12:00:00.000Z",
		"data": map[string]any{
			"providerId":            provider,
			"externalTransactionId": extID,
			"idempotencyKey":        key,
			"playerId":              playerID,
			"walletId":              walletID,
			"roundId":               "round-1",
			"gameId":                "game-1",
			"kind":                  "BET",
			"money":                 map[string]string{"amount": amount, "currency": "BRL"},
		},
	}
	raw, _ := json.Marshal(body)
	return raw
}
