package metrics

import (
	"github.com/go-kit/kit/metrics"
	"github.com/go-kit/kit/metrics/discard"
	"github.com/go-kit/kit/metrics/prometheus"
	stdprometheus "github.com/prometheus/client_golang/prometheus"
)

// Metrics represents the txpool metrics
type TxMetrics struct {
	// Pending transactions
	PendingTxs metrics.Gauge
}

// GetTxPrometheusMetrics return the txpool metrics instance
func GetTxPrometheusMetrics(namespace string, labelsWithValues ...string) *TxMetrics {
	labels := []string{}

	for i := 0; i < len(labelsWithValues); i += 2 {
		labels = append(labels, labelsWithValues[i])
	}

	return &TxMetrics{
		PendingTxs: prometheus.NewGaugeFrom(stdprometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "txpool",
			Name:      "pending_transactions",
			Help:      "Pending transactions in the pool",
		}, labels).With(labelsWithValues...),
	}
}

// NewTxMetrics will return the non operational txpool metrics
func NewTxMetrics() *TxMetrics {
	return &TxMetrics{
		PendingTxs: discard.NewGauge(),
	}
}
