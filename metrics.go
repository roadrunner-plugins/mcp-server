package mcp

import (
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

// StatsExporter exports metrics for Prometheus
type StatsExporter struct {
	plugin *Plugin

	// Tool metrics
	toolsRegistered *prometheus.Desc
	toolCalls       *prometheus.Desc
	toolDuration    *prometheus.Desc
	toolErrors      *prometheus.Desc

	// Session metrics
	activeSessions *prometheus.Desc
	totalSessions  *prometheus.Desc

	// Worker metrics
	workersTotal  *prometheus.Desc
	workersActive *prometheus.Desc
	workersIdle   *prometheus.Desc
}

// newStatsExporter creates a new stats exporter
func newStatsExporter(p *Plugin) *StatsExporter {
	return &StatsExporter{
		plugin: p,

		toolsRegistered: prometheus.NewDesc(
			prometheus.BuildFQName("mcp", "", "tools_registered"),
			"Total number of registered tools",
			nil,
			nil,
		),

		toolCalls: prometheus.NewDesc(
			prometheus.BuildFQName("mcp", "", "tool_calls_total"),
			"Total number of tool calls",
			[]string{"tool", "status"},
			nil,
		),

		toolDuration: prometheus.NewDesc(
			prometheus.BuildFQName("mcp", "", "tool_duration_seconds"),
			"Tool execution duration in seconds",
			[]string{"tool"},
			nil,
		),

		toolErrors: prometheus.NewDesc(
			prometheus.BuildFQName("mcp", "", "tool_errors_total"),
			"Total number of tool execution errors",
			[]string{"tool"},
			nil,
		),

		activeSessions: prometheus.NewDesc(
			prometheus.BuildFQName("mcp", "", "active_sessions"),
			"Number of active MCP sessions",
			[]string{"transport"},
			nil,
		),

		totalSessions: prometheus.NewDesc(
			prometheus.BuildFQName("mcp", "", "sessions_total"),
			"Total number of sessions created",
			[]string{"transport"},
			nil,
		),

		workersTotal: prometheus.NewDesc(
			prometheus.BuildFQName("mcp", "", "workers_total"),
			"Total number of PHP workers",
			nil,
			nil,
		),

		workersActive: prometheus.NewDesc(
			prometheus.BuildFQName("mcp", "", "workers_active"),
			"Number of active PHP workers",
			nil,
			nil,
		),

		workersIdle: prometheus.NewDesc(
			prometheus.BuildFQName("mcp", "", "workers_idle"),
			"Number of idle PHP workers",
			nil,
			nil,
		),
	}
}

// Describe implements prometheus.Collector
func (s *StatsExporter) Describe(ch chan<- *prometheus.Desc) {
	ch <- s.toolsRegistered
	ch <- s.toolCalls
	ch <- s.toolDuration
	ch <- s.toolErrors
	ch <- s.activeSessions
	ch <- s.totalSessions
	ch <- s.workersTotal
	ch <- s.workersActive
	ch <- s.workersIdle
}

// Collect implements prometheus.Collector
func (s *StatsExporter) Collect(ch chan<- prometheus.Metric) {
	s.plugin.mu.RLock()
	defer s.plugin.mu.RUnlock()

	// Tools registered
	ch <- prometheus.MustNewConstMetric(
		s.toolsRegistered,
		prometheus.GaugeValue,
		float64(len(s.plugin.tools)),
	)

	// Active sessions by transport
	sessionsByTransport := make(map[string]int)
	for _, info := range s.plugin.sessions {
		sessionsByTransport[info.Transport]++
	}

	for transport, count := range sessionsByTransport {
		ch <- prometheus.MustNewConstMetric(
			s.activeSessions,
			prometheus.GaugeValue,
			float64(count),
			transport,
		)
	}

	// Worker metrics
	if s.plugin.pool != nil {
		workers := s.plugin.Workers()
		totalWorkers := len(workers)
		activeWorkers := 0
		idleWorkers := 0

		for _, state := range workers {
			// Assuming WorkerState has a Status field or similar
			// Adjust based on actual API
			if state.Status == "active" || state.NumExecs > 0 {
				activeWorkers++
			} else {
				idleWorkers++
			}
		}

		ch <- prometheus.MustNewConstMetric(
			s.workersTotal,
			prometheus.GaugeValue,
			float64(totalWorkers),
		)

		ch <- prometheus.MustNewConstMetric(
			s.workersActive,
			prometheus.GaugeValue,
			float64(activeWorkers),
		)

		ch <- prometheus.MustNewConstMetric(
			s.workersIdle,
			prometheus.GaugeValue,
			float64(idleWorkers),
		)
	}
}

// logMetrics logs current metrics
func (s *StatsExporter) logMetrics() {
	s.plugin.mu.RLock()
	defer s.plugin.mu.RUnlock()

	s.plugin.log.Info("current metrics",
		zap.Int("tools_registered", len(s.plugin.tools)),
		zap.Int("active_sessions", len(s.plugin.sessions)),
	)
}
