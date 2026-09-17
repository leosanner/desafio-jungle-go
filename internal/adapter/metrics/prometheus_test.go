package metricsadapter

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/leosanner/desafio-jungle-go/internal/app"
)

func TestPrometheusSatisfiesAppMetricsAndScrape(t *testing.T) {
	t.Parallel()
	var m app.Metrics = New()
	if _, ok := m.(interface{ Handler() http.Handler }); !ok {
		t.Fatal("Prometheus must expose Handler through app.Metrics")
	}
}

func TestPrometheusCatalogAndScrape(t *testing.T) {
	t.Parallel()
	p := New()
	p.IncReconciliationDivergence()
	p.ObserveHTTP(http.MethodPost, "POST /wagering/transactions", http.StatusOK, 12*time.Millisecond)
	p.ObserveOperation("BET", "PROCESSED", app.MetricChannelHTTP, 8*time.Millisecond)
	p.IncDuplicate("BET", app.MetricChannelHTTP)
	p.IncConflict(app.ConflictIdempotencyPayload)
	p.IncSQSRetry()
	p.IncSQSDLQ()
	p.SetOutboxLag(3, 5*time.Second)
	p.IncOutboxPublished(2)
	p.IncOutboxPublishFailed(1)

	rec := httptest.NewRecorder()
	p.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body, err := io.ReadAll(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, name := range []string{
		"wagering_http_requests_total",
		"wagering_http_request_duration_seconds",
		"wagering_operations_total",
		"wagering_operation_duration_seconds",
		"wagering_duplicates_total",
		"wagering_conflicts_total",
		"wagering_sqs_retries_total",
		"wagering_sqs_dlq_total",
		"wagering_outbox_unpublished",
		"wagering_outbox_oldest_unpublished_seconds",
		"wagering_outbox_published_total",
		"wagering_outbox_publish_errors_total",
		"wagering_reconciliation_divergences_total",
	} {
		if !strings.Contains(text, name) {
			t.Errorf("missing metric %s", name)
		}
	}
	if !strings.Contains(text, `wagering_conflicts_total{reason="idempotency_payload"}`) {
		t.Errorf("conflict sample missing:\n%s", text)
	}
	if !strings.Contains(text, `wagering_outbox_unpublished 3`) {
		t.Errorf("unpublished gauge missing:\n%s", text)
	}
}
