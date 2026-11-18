package plugin

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/nscaledev/nvidia-platform-chart/internal/fabricstate"
)

// Skip reasons for nccl_fabric_nri_containers_skipped_total.
const (
	reasonUserSet    = "user_set"
	reasonNotIndexed = "not_indexed"
	reasonError      = "error"
)

// Metrics are the plugin's Prometheus metrics. All methods are safe on a nil
// receiver.
type Metrics struct {
	nodeState *prometheus.GaugeVec
	adjusted  prometheus.Counter
	skipped   *prometheus.CounterVec
}

// NewMetrics creates the metrics and registers them with reg.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		nodeState: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "nccl_fabric_nri_node_state",
			Help: "1 for the node's current InfiniBand p_key layout state, 0 for the others.",
		}, []string{"state"}),
		adjusted: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "nccl_fabric_nri_containers_adjusted_total",
			Help: "Containers given NCCL_IB_PKEY.",
		}),
		skipped: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "nccl_fabric_nri_containers_skipped_total",
			Help: "Containers left unchanged, by reason.",
		}, []string{"reason"}),
	}

	for _, k := range fabricstate.Kinds {
		m.nodeState.WithLabelValues(string(k)).Set(0)
	}

	for _, r := range []string{reasonUserSet, reasonNotIndexed, reasonError} {
		m.skipped.WithLabelValues(r)
	}

	reg.MustRegister(m.nodeState, m.adjusted, m.skipped)

	return m
}

// SetNodeState marks k as the current state and every other state as 0.
func (m *Metrics) SetNodeState(k fabricstate.Kind) {
	if m == nil {
		return
	}

	for _, other := range fabricstate.Kinds {
		v := 0.0
		if other == k {
			v = 1
		}

		m.nodeState.WithLabelValues(string(other)).Set(v)
	}
}

func (m *Metrics) countAdjusted() {
	if m != nil {
		m.adjusted.Inc()
	}
}

func (m *Metrics) countSkipped(reason string) {
	if m != nil {
		m.skipped.WithLabelValues(reason).Inc()
	}
}
