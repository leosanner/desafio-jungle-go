package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type config struct {
	Bases          []string
	Issuer         string
	InternalID     string
	InternalSecret string
	ProviderID     string
	ProviderSecret string
	DistinctN      int
	ReplayN        int
	ContentionN    int
	Concurrency    int
	HTTPTimeout    time.Duration
	OutboxWait     time.Duration
}

func configFromEnv() (config, error) {
	bases := splitBases(firstNonEmpty(os.Getenv("WAGERING_BASES"), os.Getenv("WAGERING_BASE"), "http://127.0.0.1:8080"))
	if len(bases) == 0 {
		return config{}, fmt.Errorf("WAGERING_BASE / WAGERING_BASES is empty")
	}
	issuer := strings.TrimSpace(os.Getenv("OIDC_ISSUER"))
	if issuer == "" {
		issuer = "http://localhost:8081/realms/wagering"
	}
	cfg := config{
		Bases:          bases,
		Issuer:         issuer,
		InternalID:     firstNonEmpty(os.Getenv("OIDC_INTERNAL_CLIENT"), "wagering-internal"),
		InternalSecret: firstNonEmpty(os.Getenv("LOADTEST_INTERNAL_SECRET"), "internal-secret"),
		ProviderID:     firstNonEmpty(os.Getenv("LOADTEST_PROVIDER_CLIENT"), "provider-a"),
		ProviderSecret: firstNonEmpty(os.Getenv("LOADTEST_PROVIDER_SECRET"), "provider-a-secret"),
		DistinctN:      envInt("LOADTEST_DISTINCT_WALLETS", 80),
		ReplayN:        envInt("LOADTEST_REPLAY_N", 120),
		ContentionN:    envInt("LOADTEST_CONTENTION_N", 40),
		Concurrency:    envInt("LOADTEST_CONCURRENCY", 64),
		HTTPTimeout:    envDuration("LOADTEST_HTTP_TIMEOUT", 15*time.Second),
		OutboxWait:     envDuration("LOADTEST_OUTBOX_WAIT", 30*time.Second),
	}
	return cfg, nil
}

type report struct {
	Environment     reportEnv      `json:"environment"`
	DistinctWallets scenarioReport `json:"distinct_wallets"`
	ReplayBurst     scenarioReport `json:"idempotent_replay_burst"`
	SameWalletRace  scenarioReport `json:"same_wallet_contention"`
	OutboxAfter     outboxReport   `json:"outbox_after"`
	AllInvariantsOK bool           `json:"all_invariants_ok"`
}

type reportEnv struct {
	Bases       []string `json:"bases"`
	Issuer      string   `json:"oidc_issuer"`
	Instances   int      `json:"instances"`
	Methodology string   `json:"methodology"`
}

type scenarioReport struct {
	Scenario      string         `json:"scenario"`
	Requests      int            `json:"requests"`
	WallS         float64        `json:"wall_s"`
	ThroughputRPS float64        `json:"throughput_rps"`
	Outcomes      map[string]int `json:"outcomes"`
	Latency       latencyStats   `json:"latency"`
	HTTPErrors    int            `json:"http_errors"`
	ClientErrors  int            `json:"client_errors"`
	Expected      map[string]any `json:"expected"`
	Actual        map[string]any `json:"actual"`
	Invariants    invariantCheck `json:"invariants"`
}

type outboxReport struct {
	Drained bool           `json:"drained"`
	WaitS   float64        `json:"wait_s"`
	Metrics scrapedMetrics `json:"metrics"`
}

func run(ctx context.Context, cfg config) (report, error) {
	c := newAPIClient(cfg.Bases, cfg.Issuer, cfg.HTTPTimeout)
	if err := c.ready(ctx); err != nil {
		return report{}, err
	}
	internal, err := c.login(ctx, cfg.InternalID, cfg.InternalSecret)
	if err != nil {
		return report{}, fmt.Errorf("internal token: %w (OIDC_ISSUER must match the process iss)", err)
	}
	provider, err := c.login(ctx, cfg.ProviderID, cfg.ProviderSecret)
	if err != nil {
		return report{}, fmt.Errorf("provider token: %w", err)
	}
	c.internal, c.provider = internal, provider

	out := report{
		Environment: reportEnv{
			Bases:     cfg.Bases,
			Issuer:    cfg.Issuer,
			Instances: len(cfg.Bases),
			Methodology: "Closed authenticated HTTP bursts against a running wagering process. " +
				"Client-side latency. Invariants via GET wallet + POST reconciliation. " +
				"Outbox lag from GET /metrics. Amounts are JSON strings. No RPS target.",
		},
	}

	distinct, err := runDistinct(ctx, c, cfg)
	if err != nil {
		return report{}, err
	}
	out.DistinctWallets = distinct

	replay, err := runReplay(ctx, c, cfg)
	if err != nil {
		return report{}, err
	}
	out.ReplayBurst = replay

	race, err := runContention(ctx, c, cfg)
	if err != nil {
		return report{}, err
	}
	out.SameWalletRace = race

	outbox, err := waitOutbox(ctx, c, cfg.OutboxWait)
	if err != nil {
		return report{}, err
	}
	out.OutboxAfter = outbox
	out.AllInvariantsOK = distinct.Invariants.OK && replay.Invariants.OK && race.Invariants.OK && outbox.Drained
	return out, nil
}

func runDistinct(ctx context.Context, c *apiClient, cfg config) (scenarioReport, error) {
	wallets := make([]walletInfo, cfg.DistinctN)
	for i := 0; i < cfg.DistinctN; i++ {
		w, err := c.openWallet(ctx, uniqueID("player-"), "100.00")
		if err != nil {
			return scenarioReport{}, err
		}
		wallets[i] = w
	}
	samples, wall := burst(cfg.Concurrency, cfg.DistinctN, func(i int) sample {
		ext := uniqueID("bet-a-")
		return c.bet(ctx, i, wallets[i].PlayerID, wallets[i].ID, ext, "10.00", "provider-a:"+ext)
	})
	balances := make([]string, len(wallets))
	consistent := true
	for i, w := range wallets {
		got, err := c.getWallet(ctx, w.ID)
		if err != nil {
			return scenarioReport{}, err
		}
		balances[i] = got.Balance.Amount
		rec, err := c.reconcile(ctx, w.ID)
		if err != nil {
			return scenarioReport{}, err
		}
		if !rec.Consistent {
			consistent = false
		}
	}
	inv := checkDistinct(samples, balances, consistent, cfg.DistinctN, "90.00")
	return summarize("distinct_wallets", samples, wall, map[string]any{
		"processed": cfg.DistinctN,
		"balance":   "90.00",
	}, map[string]any{
		"processed": countByClass(samples)["processed"],
		"balances":  uniqueStrings(balances),
	}, inv), nil
}

func runReplay(ctx context.Context, c *apiClient, cfg config) (scenarioReport, error) {
	w, err := c.openWallet(ctx, uniqueID("player-"), "100.00")
	if err != nil {
		return scenarioReport{}, err
	}
	ext := uniqueID("bet-b-")
	key := "provider-a:" + ext
	samples, wall := burst(cfg.Concurrency, cfg.ReplayN, func(i int) sample {
		return c.bet(ctx, i, w.PlayerID, w.ID, ext, "10.00", key)
	})
	got, err := c.getWallet(ctx, w.ID)
	if err != nil {
		return scenarioReport{}, err
	}
	rec, err := c.reconcile(ctx, w.ID)
	if err != nil {
		return scenarioReport{}, err
	}
	classes := countByClass(samples)
	inv := checkReplay(samples, got.Balance.Amount, rec.Consistent, cfg.ReplayN, "90.00")
	return summarize("idempotent_replay_burst", samples, wall, map[string]any{
		"processed": 1,
		"replay":    cfg.ReplayN - 1,
		"balance":   "90.00",
	}, map[string]any{
		"processed": classes["processed"],
		"replay":    classes["replay"],
		"balance":   got.Balance.Amount,
	}, inv), nil
}

func runContention(ctx context.Context, c *apiClient, cfg config) (scenarioReport, error) {
	w, err := c.openWallet(ctx, uniqueID("player-"), "100.00")
	if err != nil {
		return scenarioReport{}, err
	}
	samples, wall := burst(cfg.Concurrency, cfg.ContentionN, func(i int) sample {
		ext := uniqueID("bet-c-")
		return c.bet(ctx, i, w.PlayerID, w.ID, ext, "10.00", "provider-a:"+ext)
	})
	got, err := c.getWallet(ctx, w.ID)
	if err != nil {
		return scenarioReport{}, err
	}
	rec, err := c.reconcile(ctx, w.ID)
	if err != nil {
		return scenarioReport{}, err
	}
	classes := countByClass(samples)
	wantProcessed := 10
	wantRejected := cfg.ContentionN - wantProcessed
	inv := checkContention(samples, got.Balance.Amount, rec.Consistent, wantProcessed, wantRejected, "0.00")
	return summarize("same_wallet_contention", samples, wall, map[string]any{
		"processed":             wantProcessed,
		"rejected_insufficient": wantRejected,
		"balance":               "0.00",
	}, map[string]any{
		"processed":             classes["processed"],
		"rejected_insufficient": classes["rejected:INSUFFICIENT_FUNDS"],
		"balance":               got.Balance.Amount,
	}, inv), nil
}

func waitOutbox(ctx context.Context, c *apiClient, deadline time.Duration) (outboxReport, error) {
	started := time.Now()
	for {
		m, err := c.scrape(ctx)
		if err != nil {
			return outboxReport{}, err
		}
		if m.Unpublished == 0 {
			return outboxReport{Drained: true, WaitS: round2(time.Since(started).Seconds()), Metrics: m}, nil
		}
		if time.Since(started) >= deadline {
			return outboxReport{Drained: false, WaitS: round2(time.Since(started).Seconds()), Metrics: m}, nil
		}
		select {
		case <-ctx.Done():
			return outboxReport{}, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func burst(workers, n int, fn func(i int) sample) ([]sample, time.Duration) {
	if workers < 1 {
		workers = 1
	}
	if workers > n {
		workers = n
	}
	out := make([]sample, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	sem := make(chan struct{}, workers)
	begun := time.Now()
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			sem <- struct{}{}
			out[i] = fn(i)
			<-sem
		}(i)
	}
	close(start)
	wg.Wait()
	return out, time.Since(begun)
}

func summarize(name string, samples []sample, wall time.Duration, expected, actual map[string]any, inv invariantCheck) scenarioReport {
	server, client := httpErrorCount(samples)
	wallS := wall.Seconds()
	rps := 0.0
	if wallS > 0 {
		rps = round2(float64(len(samples)) / wallS)
	}
	return scenarioReport{
		Scenario:      name,
		Requests:      len(samples),
		WallS:         round2(wallS),
		ThroughputRPS: rps,
		Outcomes:      countByClass(samples),
		Latency:       latencyOf(samples),
		HTTPErrors:    server,
		ClientErrors:  client,
		Expected:      expected,
		Actual:        actual,
		Invariants:    inv,
	}
}

func uniqueID(prefix string) string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return prefix + hex.EncodeToString(b[:])
}

func uniqueStrings(in []string) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func splitBases(raw string) []string {
	parts := strings.Split(raw, ",")
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func envInt(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return fallback
	}
	return n
}

func envDuration(key string, fallback time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return fallback
	}
	return d
}
