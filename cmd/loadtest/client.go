package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type apiClient struct {
	http     *http.Client
	bases    []string
	issuer   string
	internal string
	provider string
}

type walletInfo struct {
	ID       string `json:"id"`
	PlayerID string `json:"playerId"`
	Balance  struct {
		Amount   string `json:"amount"`
		Currency string `json:"currency"`
	} `json:"balance"`
}

type reconBody struct {
	Consistent    bool `json:"consistent"`
	StoredBalance struct {
		Amount string `json:"amount"`
	}
	CalculatedBalance struct {
		Amount string `json:"amount"`
	}
}

func newAPIClient(bases []string, issuer string, timeout time.Duration) *apiClient {
	return &apiClient{
		http:   &http.Client{Timeout: timeout},
		bases:  bases,
		issuer: strings.TrimRight(issuer, "/"),
	}
}

func (c *apiClient) base(i int) string {
	return strings.TrimRight(c.bases[i%len(c.bases)], "/")
}

func (c *apiClient) login(ctx context.Context, clientID, secret string) (string, error) {
	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {clientID},
		"client_secret": {secret},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.issuer+"/protocol/openid-connect/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token endpoint status %d", resp.StatusCode)
	}
	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.AccessToken == "" {
		return "", fmt.Errorf("token endpoint: missing access_token")
	}
	return out.AccessToken, nil
}

func (c *apiClient) ready(ctx context.Context) error {
	for i, b := range c.bases {
		status, raw, err := c.do(ctx, http.MethodGet, strings.TrimRight(b, "/")+"/health/ready", nil, nil)
		if err != nil {
			return fmt.Errorf("ready %s: %w", b, err)
		}
		if status != http.StatusOK {
			return fmt.Errorf("ready %s: status %d %s", c.bases[i], status, truncate(raw, 200))
		}
	}
	return nil
}

func (c *apiClient) openWallet(ctx context.Context, playerID, amount string) (walletInfo, error) {
	body := map[string]any{
		"playerId":       playerID,
		"initialBalance": map[string]string{"amount": amount, "currency": "BRL"},
	}
	status, raw, err := c.doJSON(ctx, http.MethodPost, c.base(0)+"/wallets", c.internal, nil, body)
	if err != nil {
		return walletInfo{}, err
	}
	if status != http.StatusCreated {
		return walletInfo{}, fmt.Errorf("open wallet status %d %s", status, truncate(raw, 300))
	}
	var w walletInfo
	if err := json.Unmarshal(raw, &w); err != nil {
		return walletInfo{}, err
	}
	w.PlayerID = playerID
	return w, nil
}

func (c *apiClient) getWallet(ctx context.Context, walletID string) (walletInfo, error) {
	status, raw, err := c.doJSON(ctx, http.MethodGet, c.base(0)+"/wallets/"+walletID, c.internal, nil, nil)
	if err != nil {
		return walletInfo{}, err
	}
	if status != http.StatusOK {
		return walletInfo{}, fmt.Errorf("get wallet status %d %s", status, truncate(raw, 300))
	}
	var w walletInfo
	if err := json.Unmarshal(raw, &w); err != nil {
		return walletInfo{}, err
	}
	return w, nil
}

func (c *apiClient) reconcile(ctx context.Context, walletID string) (reconBody, error) {
	status, raw, err := c.doJSON(ctx, http.MethodPost, c.base(0)+"/wallets/"+walletID+"/reconciliation", c.internal, nil, nil)
	if err != nil {
		return reconBody{}, err
	}
	if status != http.StatusOK {
		return reconBody{}, fmt.Errorf("reconcile status %d %s", status, truncate(raw, 300))
	}
	var rec reconBody
	if err := json.Unmarshal(raw, &rec); err != nil {
		return reconBody{}, err
	}
	return rec, nil
}

func (c *apiClient) bet(ctx context.Context, i int, playerID, walletID, ext, amount, key string) sample {
	body := map[string]any{
		"providerId":            "provider-a",
		"externalTransactionId": ext,
		"playerId":              playerID,
		"walletId":              walletID,
		"roundId":               "load-round",
		"gameId":                "load-game",
		"kind":                  "BET",
		"money":                 map[string]string{"amount": amount, "currency": "BRL"},
	}
	headers := map[string]string{"Idempotency-Key": key}
	started := time.Now()
	status, raw, err := c.doJSON(ctx, http.MethodPost, c.base(i)+"/wagering/transactions", c.provider, headers, body)
	elapsed := float64(time.Since(started).Microseconds()) / 1000
	if err != nil {
		return sample{Status: 0, Elapsed: elapsed, Classify: "error:0:transport"}
	}
	op := decodeOperation(raw)
	return sample{Status: status, Body: op, Elapsed: elapsed, Classify: classify(status, op)}
}

func (c *apiClient) scrape(ctx context.Context) (scrapedMetrics, error) {
	parts := make([]scrapedMetrics, 0, len(c.bases))
	for _, b := range c.bases {
		status, raw, err := c.do(ctx, http.MethodGet, strings.TrimRight(b, "/")+"/metrics", nil, nil)
		if err != nil {
			return scrapedMetrics{}, err
		}
		if status != http.StatusOK {
			return scrapedMetrics{}, fmt.Errorf("metrics %s status %d", b, status)
		}
		parts = append(parts, parseProm(string(raw)))
	}
	return mergeMetrics(parts), nil
}

func (c *apiClient) doJSON(ctx context.Context, method, url, bearer string, extra map[string]string, body any) (int, []byte, error) {
	var rdr io.Reader
	headers := map[string]string{"Accept": "application/json"}
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		rdr = bytes.NewReader(raw)
		headers["Content-Type"] = "application/json"
	}
	if bearer != "" {
		headers["Authorization"] = "Bearer " + bearer
	}
	for k, v := range extra {
		headers[k] = v
	}
	return c.do(ctx, method, url, headers, rdr)
}

func (c *apiClient) do(ctx context.Context, method, rawURL string, headers map[string]string, body io.Reader) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return 0, nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	return resp.StatusCode, raw, err
}

func truncate(raw []byte, n int) string {
	s := string(raw)
	if len(s) <= n {
		return s
	}
	return s[:n]
}
