package observability

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

const (
	OutcomeSuccess = "success"
	OutcomeError   = "error"
	OutcomeRequeue = "requeue"

	OperationCreate = "create"
	OperationUpdate = "update"
	OperationNoop   = "noop"
)

var (
	reconcileTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dns_operator_reconcile_total",
			Help: "Total reconcile attempts by controller and outcome.",
		},
		[]string{"controller", "outcome"},
	)
	reconcileDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "dns_operator_reconcile_duration_seconds",
			Help:    "Reconcile duration by controller and outcome.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"controller", "outcome"},
	)
	artifactUpdates = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dns_operator_artifact_updates_total",
			Help: "Rendered artifact update attempts by controller, artifact, and operation.",
		},
		[]string{"controller", "artifact", "operation"},
	)
)

func init() {
	metrics.Registry.MustRegister(reconcileTotal, reconcileDuration, artifactUpdates)
}

func ObserveReconcile(controller string, started time.Time, result ctrl.Result, err error) {
	outcome := OutcomeSuccess
	if err != nil {
		outcome = OutcomeError
	} else if result.Requeue || result.RequeueAfter > 0 {
		outcome = OutcomeRequeue
	}

	reconcileTotal.WithLabelValues(controller, outcome).Inc()
	reconcileDuration.WithLabelValues(controller, outcome).Observe(time.Since(started).Seconds())
}

func RecordArtifactUpdate(controller, artifact, operation string) {
	artifactUpdates.WithLabelValues(controller, artifact, operation).Inc()
}
