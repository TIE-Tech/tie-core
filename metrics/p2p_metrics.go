package metrics

import (
	"github.com/go-kit/kit/metrics"
	"github.com/go-kit/kit/metrics/discard"
	"github.com/go-kit/kit/metrics/prometheus"
	stdprometheus "github.com/prometheus/client_golang/prometheus"
)

// Metrics represents the network metrics
type P2PMetrics struct {
	// No.of connected peers
	Peers metrics.Gauge
}

// GetP2PPrometheusMetrics return the network metrics instance
func GetP2PPrometheusMetrics(namespace string, labelsWithValues ...string) *P2PMetrics {
	labels := []string{}

	for i := 0; i < len(labelsWithValues); i += 2 {
		labels = append(labels, labelsWithValues[i])
	}

	return &P2PMetrics{
		Peers: prometheus.NewGaugeFrom(stdprometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "network",
			Name:      "peers",
			Help:      "Number of connected peers.",
		}, labels).With(labelsWithValues...),
	}
}

// NewP2PMetrics will return the non operational metrics
func NewP2PMetrics() *P2PMetrics {
	return &P2PMetrics{
		Peers: discard.NewGauge(),
	}
}
