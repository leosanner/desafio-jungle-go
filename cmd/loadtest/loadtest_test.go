package main

import (
	"encoding/json"
	"testing"
)

func TestPercentileNearestRank(t *testing.T) {
	vals := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	if got := percentile(vals, 50); got != 6 {
		t.Fatalf("p50 = %v", got)
	}
	if got := percentile(vals, 95); got != 10 {
		t.Fatalf("p95 = %v", got)
	}
	if got := percentile(vals, 0); got != 1 {
		t.Fatalf("p0 = %v", got)
	}
	if got := percentile(nil, 50); got != 0 {
		t.Fatalf("empty = %v", got)
	}
}

func TestClassifyReplayAndRejection(t *testing.T) {
	if got := classify(200, operationBody{Status: "PROCESSED"}); got != "processed" {
		t.Fatalf("processed: %s", got)
	}
	if got := classify(200, operationBody{Status: "PROCESSED", IdempotentReplay: true}); got != "replay" {
		t.Fatalf("replay: %s", got)
	}
	if got := classify(422, operationBody{Status: "REJECTED", FailureCode: "INSUFFICIENT_FUNDS"}); got != "rejected:INSUFFICIENT_FUNDS" {
		t.Fatalf("rejected: %s", got)
	}
	if got := classify(401, operationBody{Error: "unauthenticated"}); got != "error:401:unauthenticated" {
		t.Fatalf("401: %s", got)
	}
}

func TestCheckReplayAndContention(t *testing.T) {
	samples := make([]sample, 5)
	samples[0] = sample{Classify: "processed"}
	for i := 1; i < 5; i++ {
		samples[i] = sample{Classify: "replay"}
	}
	if !checkReplay(samples, "90.00", true, 5, "90.00").OK {
		t.Fatal("replay should pass")
	}
	if checkReplay(samples, "80.00", true, 5, "90.00").OK {
		t.Fatal("wrong balance should fail")
	}

	race := make([]sample, 4)
	race[0] = sample{Classify: "processed"}
	race[1] = sample{Classify: "processed"}
	race[2] = sample{Classify: "rejected:INSUFFICIENT_FUNDS"}
	race[3] = sample{Classify: "rejected:INSUFFICIENT_FUNDS"}
	if !checkContention(race, "0.00", true, 2, 2, "0.00").OK {
		t.Fatal("contention should pass")
	}
}

func TestCheckDistinct(t *testing.T) {
	samples := []sample{{Classify: "processed"}, {Classify: "processed"}}
	ok := checkDistinct(samples, []string{"90.00", "90.00"}, true, 2, "90.00")
	if !ok.OK {
		t.Fatalf("%v", ok.Errors)
	}
	bad := checkDistinct(samples, []string{"90.00", "80.00"}, true, 2, "90.00")
	if bad.OK {
		t.Fatal("mixed balances should fail")
	}
}

func TestParsePromLabeledAndGauge(t *testing.T) {
	text := `# HELP wagering_outbox_unpublished rows
# TYPE wagering_outbox_unpublished gauge
wagering_outbox_unpublished 3
wagering_outbox_oldest_unpublished_seconds 1.5
wagering_conflicts_total{reason="optimistic_lock"} 2
wagering_duplicates_total{channel="http",kind="BET"} 119
wagering_reconciliation_divergences_total 0
`
	got := parseProm(text)
	if got.Unpublished != 3 || got.OldestUnpublishedSeconds != 1.5 {
		t.Fatalf("gauges %+v", got)
	}
	if got.ConflictsOptimisticLock != 2 || got.DuplicatesBetHTTP != 119 {
		t.Fatalf("labeled %+v", got)
	}
	if got.DivergencesTotal != 0 {
		t.Fatalf("divergences %+v", got)
	}
}

func TestMergeMetricsTakesMaxUnpublishedAndSumsCounters(t *testing.T) {
	got := mergeMetrics([]scrapedMetrics{
		{Unpublished: 4, DuplicatesBetHTTP: 10, PublishedTotal: 1},
		{Unpublished: 1, DuplicatesBetHTTP: 5, PublishedTotal: 2, OldestUnpublishedSeconds: 9},
	})
	if got.Unpublished != 4 {
		t.Fatalf("unpublished %v", got.Unpublished)
	}
	if got.DuplicatesBetHTTP != 15 || got.PublishedTotal != 3 {
		t.Fatalf("sums %+v", got)
	}
	if got.OldestUnpublishedSeconds != 9 {
		t.Fatalf("oldest %v", got.OldestUnpublishedSeconds)
	}
}

func TestDecodeOperationJSON(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{"status": "PROCESSED", "idempotentReplay": true})
	body := decodeOperation(raw)
	if !body.IdempotentReplay || body.Status != "PROCESSED" {
		t.Fatalf("%+v", body)
	}
}

func TestSplitBases(t *testing.T) {
	got := splitBases("http://127.0.0.1:8080, http://127.0.0.1:8082,")
	if len(got) != 2 || got[1] != "http://127.0.0.1:8082" {
		t.Fatalf("%v", got)
	}
}
