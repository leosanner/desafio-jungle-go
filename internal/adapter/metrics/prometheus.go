package metricsadapter

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/leosanner/desafio-jungle-go/internal/app"
)

var buckets = []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10}

// Prometheus is the OBS-02 adapter. Each instance uses a private registry.
type Prometheus struct {
	reg *prometheus.Registry

	httpRequests      *prometheus.CounterVec
	httpDuration      *prometheus.HistogramVec
	operations        *prometheus.CounterVec
	operationDuration *prometheus.HistogramVec
	duplicates        *prometheus.CounterVec
	conflicts         *prometheus.CounterVec
	sqsRetries        prometheus.Counter
	sqsDLQ            prometheus.Counter
	outboxUnpublished prometheus.Gauge
	outboxOldest      prometheus.Gauge
	outboxPublished   prometheus.Counter
	outboxErrors      prometheus.Counter
	reconciliation    prometheus.Counter
}

var _ app.Metrics = (*Prometheus)(nil)

// New constructs collectors on a process-local registry (not the global default).
func New() *Prometheus {
	reg := prometheus.NewRegistry()
	p := &Prometheus{
		reg: reg,
		httpRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "wagering_http_requests_total",
			Help: "Finished business HTTP requests.",
		}, []string{"method", "pattern", "code"}),
		httpDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "wagering_http_request_duration_seconds",
			Help:    "Business HTTP request latency in seconds.",
			Buckets: buckets,
		}, []string{"method", "pattern"}),
		operations: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "wagering_operations_total",
			Help: "Persisted wagering operation outcomes.",
		}, []string{"kind", "status", "channel"}),
		operationDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "wagering_operation_duration_seconds",
			Help:    "Submit / HandleInbound wall time in seconds.",
			Buckets: buckets,
		}, []string{"kind", "channel"}),
		duplicates: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "wagering_duplicates_total",
			Help: "Idempotent replays of a persisted operation.",
		}, []string{"kind", "channel"}),
		conflicts: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "wagering_conflicts_total",
			Help: "Idempotency, identity and concurrency conflicts.",
		}, []string{"reason"}),
		sqsRetries: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "wagering_sqs_retries_total",
			Help: "Inbound SQS visibility backoffs.",
		}),
		sqsDLQ: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "wagering_sqs_dlq_total",
			Help: "Inbound messages sent to the DLQ.",
		}),
		outboxUnpublished: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "wagering_outbox_unpublished",
			Help: "Outbox rows with published_at IS NULL.",
		}),
		outboxOldest: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "wagering_outbox_oldest_unpublished_seconds",
			Help: "Age in seconds of the oldest unpublished outbox row.",
		}),
		outboxPublished: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "wagering_outbox_published_total",
			Help: "Outbox rows marked published.",
		}),
		outboxErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "wagering_outbox_publish_errors_total",
			Help: "Outbox publish failures that scheduled retry.",
		}),
		reconciliation: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "wagering_reconciliation_divergences_total",
			Help: "Wallet reconciliations where stored balance ≠ ledger sum.",
		}),
	}
	reg.MustRegister(
		p.httpRequests,
		p.httpDuration,
		p.operations,
		p.operationDuration,
		p.duplicates,
		p.conflicts,
		p.sqsRetries,
		p.sqsDLQ,
		p.outboxUnpublished,
		p.outboxOldest,
		p.outboxPublished,
		p.outboxErrors,
		p.reconciliation,
	)
	return p
}

// Handler serves the Prometheus text exposition format.
func (p *Prometheus) Handler() http.Handler {
	return promhttp.HandlerFor(p.reg, promhttp.HandlerOpts{EnableOpenMetrics: true})
}

func (p *Prometheus) IncReconciliationDivergence() {
	p.reconciliation.Inc()
}

func (p *Prometheus) ObserveHTTP(method, pattern string, status int, d time.Duration) {
	code := strconv.Itoa(status)
	p.httpRequests.WithLabelValues(method, pattern, code).Inc()
	p.httpDuration.WithLabelValues(method, pattern).Observe(d.Seconds())
}

func (p *Prometheus) ObserveOperation(kind, status, channel string, d time.Duration) {
	p.operations.WithLabelValues(kind, status, channel).Inc()
	p.operationDuration.WithLabelValues(kind, channel).Observe(d.Seconds())
}

func (p *Prometheus) IncDuplicate(kind, channel string) {
	p.duplicates.WithLabelValues(kind, channel).Inc()
}

func (p *Prometheus) IncConflict(reason string) {
	p.conflicts.WithLabelValues(reason).Inc()
}

func (p *Prometheus) IncSQSRetry() { p.sqsRetries.Inc() }

func (p *Prometheus) IncSQSDLQ() { p.sqsDLQ.Inc() }

func (p *Prometheus) SetOutboxLag(unpublished int, oldest time.Duration) {
	p.outboxUnpublished.Set(float64(unpublished))
	p.outboxOldest.Set(oldest.Seconds())
}

func (p *Prometheus) IncOutboxPublished(n int) {
	if n > 0 {
		p.outboxPublished.Add(float64(n))
	}
}

func (p *Prometheus) IncOutboxPublishFailed(n int) {
	if n > 0 {
		p.outboxErrors.Add(float64(n))
	}
}
