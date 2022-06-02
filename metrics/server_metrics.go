package metrics

// ServerMetrics holds the metric instances of all sub systems
type ServerMetrics struct {
	Consensus *CosMetrics
	Network   *P2PMetrics
	Txpool    *TxMetrics
}

// metricProvider serverMetric instance for the given ChainID and nameSpace
func MetricProvider(nameSpace string, chainID string, metricsRequired bool) *ServerMetrics {
	if metricsRequired {
		return &ServerMetrics{
			Consensus: GetCosPrometheusMetrics(nameSpace, "chain_id", chainID),
			Network:   GetP2PPrometheusMetrics(nameSpace, "chain_id", chainID),
			Txpool:    GetTxPrometheusMetrics(nameSpace, "chain_id", chainID),
		}
	}

	return &ServerMetrics{
		Consensus: NewCosMetrics(),
		Network:   NewP2PMetrics(),
		Txpool:    NewTxMetrics(),
	}
}
