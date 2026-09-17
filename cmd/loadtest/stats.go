package main

import (
	"encoding/json"
	"math"
	"strconv"
)

type latencyStats struct {
	P50MS  float64 `json:"p50_ms"`
	P95MS  float64 `json:"p95_ms"`
	P99MS  float64 `json:"p99_ms"`
	MaxMS  float64 `json:"max_ms"`
	MeanMS float64 `json:"mean_ms"`
}

type sample struct {
	Status   int
	Body     operationBody
	Elapsed  float64
	Classify string
}

type operationBody struct {
	Status           string `json:"status"`
	FailureCode      string `json:"failureCode"`
	IdempotentReplay bool   `json:"idempotentReplay"`
	Error            string `json:"error"`
}

func classify(status int, body operationBody) string {
	if body.IdempotentReplay {
		return "replay"
	}
	if status == 200 && body.Status == "PROCESSED" {
		return "processed"
	}
	if body.Status == "REJECTED" {
		code := body.FailureCode
		if code == "" {
			code = "unknown"
		}
		return "rejected:" + code
	}
	if body.Error != "" {
		return "error:" + strconv.Itoa(status) + ":" + body.Error
	}
	return "other:" + strconv.Itoa(status)
}

func countByClass(samples []sample) map[string]int {
	out := make(map[string]int)
	for _, s := range samples {
		out[s.Classify]++
	}
	return out
}

func latencyOf(samples []sample) latencyStats {
	if len(samples) == 0 {
		return latencyStats{}
	}
	vals := make([]float64, len(samples))
	sum := 0.0
	for i, s := range samples {
		vals[i] = s.Elapsed
		sum += s.Elapsed
	}
	sortFloats(vals)
	return latencyStats{
		P50MS:  round2(percentile(vals, 50)),
		P95MS:  round2(percentile(vals, 95)),
		P99MS:  round2(percentile(vals, 99)),
		MaxMS:  round2(vals[len(vals)-1]),
		MeanMS: round2(sum / float64(len(vals))),
	}
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 100 {
		return sorted[len(sorted)-1]
	}
	idx := int(math.Round((p / 100) * float64(len(sorted)-1)))
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func sortFloats(vals []float64) {
	for i := 1; i < len(vals); i++ {
		v := vals[i]
		j := i
		for j > 0 && vals[j-1] > v {
			vals[j] = vals[j-1]
			j--
		}
		vals[j] = v
	}
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

func httpErrorCount(samples []sample) (server, client int) {
	for _, s := range samples {
		switch {
		case s.Status >= 500:
			server++
		case s.Status >= 400:
			client++
		}
	}
	return server, client
}

type invariantCheck struct {
	OK     bool     `json:"ok"`
	Errors []string `json:"errors,omitempty"`
}

func checkDistinct(samples []sample, balances []string, allConsistent bool, wantN int, wantBalance string) invariantCheck {
	var errs []string
	processed := 0
	for _, s := range samples {
		if s.Classify == "processed" {
			processed++
		}
	}
	if processed != wantN {
		errs = append(errs, "processed="+strconv.Itoa(processed)+" want "+strconv.Itoa(wantN))
	}
	for _, b := range balances {
		if b != wantBalance {
			errs = append(errs, "balance="+b+" want "+wantBalance)
			break
		}
	}
	if !allConsistent {
		errs = append(errs, "reconciliation diverged")
	}
	return invariantCheck{OK: len(errs) == 0, Errors: errs}
}

func checkReplay(samples []sample, balance string, consistent bool, wantN int, wantBalance string) invariantCheck {
	var errs []string
	processed, replay := 0, 0
	for _, s := range samples {
		switch s.Classify {
		case "processed":
			processed++
		case "replay":
			replay++
		}
	}
	if processed != 1 {
		errs = append(errs, "processed="+strconv.Itoa(processed)+" want 1")
	}
	if replay != wantN-1 {
		errs = append(errs, "replay="+strconv.Itoa(replay)+" want "+strconv.Itoa(wantN-1))
	}
	if balance != wantBalance {
		errs = append(errs, "balance="+balance+" want "+wantBalance)
	}
	if !consistent {
		errs = append(errs, "reconciliation diverged")
	}
	return invariantCheck{OK: len(errs) == 0, Errors: errs}
}

func checkContention(samples []sample, balance string, consistent bool, wantProcessed int, wantRejected int, wantBalance string) invariantCheck {
	var errs []string
	processed, rejected := 0, 0
	for _, s := range samples {
		switch s.Classify {
		case "processed":
			processed++
		case "rejected:INSUFFICIENT_FUNDS":
			rejected++
		}
	}
	if processed != wantProcessed {
		errs = append(errs, "processed="+strconv.Itoa(processed)+" want "+strconv.Itoa(wantProcessed))
	}
	if rejected != wantRejected {
		errs = append(errs, "rejected="+strconv.Itoa(rejected)+" want "+strconv.Itoa(wantRejected))
	}
	if balance != wantBalance {
		errs = append(errs, "balance="+balance+" want "+wantBalance)
	}
	if !consistent {
		errs = append(errs, "reconciliation diverged")
	}
	return invariantCheck{OK: len(errs) == 0, Errors: errs}
}

func decodeOperation(raw []byte) operationBody {
	var body operationBody
	_ = json.Unmarshal(raw, &body)
	return body
}
