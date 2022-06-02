package metrics

import (
	"github.com/go-kit/kit/metrics"
	"github.com/go-kit/kit/metrics/discard"
	"github.com/go-kit/kit/metrics/prometheus"
	stdprometheus "github.com/prometheus/client_golang/prometheus"
)

// Metrics represents the consensus metrics
type CosMetrics struct {
	// No.of validators
	Validators metrics.Gauge
	// No.of rounds
	Rounds metrics.Gauge
	// No.of transactions in the block
	NumTxs metrics.Gauge

	//Time between current block and the previous block in seconds
	BlockInterval metrics.Gauge
}

// GetCosPrometheusMetrics return the consensus metrics instance
func GetCosPrometheusMetrics(namespace string, labelsWithValues ...string) *CosMetrics {
	labels := []string{}

	for i := 0; i < len(labelsWithValues); i += 2 {
		labels = append(labels, labelsWithValues[i])
	}

	return &CosMetrics{
		Validators: prometheus.NewGaugeFrom(stdprometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "consensus",
			Name:      "validators",
			Help:      "Number of validators.",
		}, labels).With(labelsWithValues...),
		Rounds: prometheus.NewGaugeFrom(stdprometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "consensus",
			Name:      "rounds",
			Help:      "Number of rounds.",
		}, labels).With(labelsWithValues...),
		NumTxs: prometheus.NewGaugeFrom(stdprometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "consensus",
			Name:      "num_txs",
			Help:      "Number of transactions.",
		}, labels).With(labelsWithValues...),

		BlockInterval: prometheus.NewGaugeFrom(stdprometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "consensus",
			Name:      "block_interval",
			Help:      "Time between current block and the previous block in seconds.",
		}, labels).With(labelsWithValues...),
	}
}

// NewCosMetrics will return the non operational metrics
func NewCosMetrics() *CosMetrics {
	return &CosMetrics{
		Validators:    discard.NewGauge(),
		Rounds:        discard.NewGauge(),
		NumTxs:        discard.NewGauge(),
		BlockInterval: discard.NewGauge(),
	}
}
