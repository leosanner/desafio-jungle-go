package main

import (
	"strconv"
	"strings"
)

type scrapedMetrics struct {
	Unpublished                float64 `json:"unpublished"`
	OldestUnpublishedSeconds   float64 `json:"oldest_unpublished_seconds"`
	PublishedTotal             float64 `json:"published_total"`
	PublishErrorsTotal         float64 `json:"publish_errors_total"`
	ConflictsOptimisticLock    float64 `json:"conflicts_optimistic_lock"`
	ConflictsIdempotency       float64 `json:"conflicts_idempotency_payload"`
	ConflictsDuplicateExternal float64 `json:"conflicts_duplicate_external"`
	ConflictsUnavailable       float64 `json:"conflicts_unavailable"`
	DuplicatesBetHTTP          float64 `json:"duplicates_bet_http"`
	DivergencesTotal           float64 `json:"divergences_total"`
}

func parseProm(text string) scrapedMetrics {
	return scrapedMetrics{
		Unpublished:                gauge(text, "wagering_outbox_unpublished"),
		OldestUnpublishedSeconds:   gauge(text, "wagering_outbox_oldest_unpublished_seconds"),
		PublishedTotal:             gauge(text, "wagering_outbox_published_total"),
		PublishErrorsTotal:         gauge(text, "wagering_outbox_publish_errors_total"),
		ConflictsOptimisticLock:    labeled(text, "wagering_conflicts_total", map[string]string{"reason": "optimistic_lock"}),
		ConflictsIdempotency:       labeled(text, "wagering_conflicts_total", map[string]string{"reason": "idempotency_payload"}),
		ConflictsDuplicateExternal: labeled(text, "wagering_conflicts_total", map[string]string{"reason": "duplicate_external"}),
		ConflictsUnavailable:       labeled(text, "wagering_conflicts_total", map[string]string{"reason": "unavailable"}),
		DuplicatesBetHTTP:          labeled(text, "wagering_duplicates_total", map[string]string{"kind": "BET", "channel": "http"}),
		DivergencesTotal:           gauge(text, "wagering_reconciliation_divergences_total"),
	}
}

func mergeMetrics(parts []scrapedMetrics) scrapedMetrics {
	if len(parts) == 0 {
		return scrapedMetrics{}
	}
	out := parts[0]
	for i := 1; i < len(parts); i++ {
		p := parts[i]
		if p.Unpublished > out.Unpublished {
			out.Unpublished = p.Unpublished
		}
		if p.OldestUnpublishedSeconds > out.OldestUnpublishedSeconds {
			out.OldestUnpublishedSeconds = p.OldestUnpublishedSeconds
		}
		out.PublishedTotal += p.PublishedTotal
		out.PublishErrorsTotal += p.PublishErrorsTotal
		out.ConflictsOptimisticLock += p.ConflictsOptimisticLock
		out.ConflictsIdempotency += p.ConflictsIdempotency
		out.ConflictsDuplicateExternal += p.ConflictsDuplicateExternal
		out.ConflictsUnavailable += p.ConflictsUnavailable
		out.DuplicatesBetHTTP += p.DuplicatesBetHTTP
		out.DivergencesTotal += p.DivergencesTotal
	}
	return out
}

func gauge(text, name string) float64 {
	return labeled(text, name, nil)
}

// labeled reads a Prometheus text value. float64 is the wire type for scrape
// samples (counts and seconds), never a monetary amount.
func labeled(text, name string, labels map[string]string) float64 {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.HasPrefix(line, name+" ") && !strings.HasPrefix(line, name+"{") {
			continue
		}
		ok := true
		for k, v := range labels {
			if !strings.Contains(line, k+`="`+v+`"`) {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		f, err := strconv.ParseFloat(fields[len(fields)-1], 64)
		if err != nil {
			continue
		}
		return f
	}
	return 0
}
