package metrics

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
)

// MetricsCollector collects and manages Prometheus metrics for the agent
type MetricsCollector struct {
	logger *logrus.Logger

	// Health metrics
	healthStatus    *prometheus.GaugeVec
	healthCheckTime *prometheus.GaugeVec

	// P2P metrics
	peerCount       *prometheus.GaugeVec
	connectionCount *prometheus.GaugeVec

	// MCP metrics
	mcpToolsCount    *prometheus.GaugeVec
	mcpRequestsTotal *prometheus.CounterVec
	mcpErrorsTotal   *prometheus.CounterVec

	// Agent info
	agentInfo *prometheus.GaugeVec

	// Registry
	registry *prometheus.Registry

	// Remote writer
	remoteWriter *RemoteWriter

	// Current health status tracking
	currentHealthStatus string
	agentName           string
	agentVersion        string
	peerID              string

	mu sync.RWMutex
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector(logger *logrus.Logger, agentName string, agentVersion string, peerID string) *MetricsCollector {
	registry := prometheus.NewRegistry()

	collector := &MetricsCollector{
		logger:              logger,
		registry:            registry,
		agentName:           agentName,
		agentVersion:        agentVersion,
		peerID:              peerID,
		currentHealthStatus: "healthy",

		healthStatus: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "praxis_agent_health_status",
			Help: "Agent health status (1 = healthy, 0 = unhealthy)",
		}, []string{"agent_name", "agent_version", "libp2p_peer_id", "health_status"}),

		healthCheckTime: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "praxis_agent_health_check_timestamp",
			Help: "Timestamp of last health check",
		}, []string{"agent_name", "agent_version", "libp2p_peer_id", "health_status"}),

		peerCount: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "praxis_agent_peer_count",
			Help: "Number of connected P2P peers",
		}, []string{"agent_name", "agent_version", "libp2p_peer_id", "health_status"}),

		connectionCount: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "praxis_agent_connection_count",
			Help: "Number of active connections",
		}, []string{"agent_name", "agent_version", "libp2p_peer_id", "health_status"}),

		mcpToolsCount: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "praxis_agent_mcp_tools_count",
			Help: "Number of registered MCP tools",
		}, []string{"agent_name", "agent_version", "libp2p_peer_id", "health_status"}),

		mcpRequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "praxis_agent_mcp_requests_total",
			Help: "Total number of MCP requests",
		}, []string{"agent_name", "agent_version", "libp2p_peer_id", "health_status"}),

		mcpErrorsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "praxis_agent_mcp_errors_total",
			Help: "Total number of MCP errors",
		}, []string{"agent_name", "agent_version", "libp2p_peer_id", "health_status"}),

		agentInfo: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "praxis_agent_info",
			Help: "Agent information",
		}, []string{"agent_name", "agent_version", "libp2p_peer_id", "health_status"}),
	}

	// Register all metrics
	registry.MustRegister(
		collector.healthStatus,
		collector.healthCheckTime,
		collector.peerCount,
		collector.connectionCount,
		collector.mcpToolsCount,
		collector.mcpRequestsTotal,
		collector.mcpErrorsTotal,
		collector.agentInfo,
	)

	// Set agent info to 1 (it's a constant label metric)
	collector.agentInfo.WithLabelValues(agentName, agentVersion, peerID, "healthy").Set(1)

	logger.Info("Metrics collector initialized")
	return collector
}

// getLabels returns the current label values for metrics
func (c *MetricsCollector) getLabels() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return []string{c.agentName, c.agentVersion, c.peerID, c.currentHealthStatus}
}

// UpdateHealthStatus updates the health status metric
func (c *MetricsCollector) UpdateHealthStatus(healthy bool) {
	c.mu.Lock()
	oldStatus := c.currentHealthStatus
	if healthy {
		c.currentHealthStatus = "healthy"
	} else {
		c.currentHealthStatus = "unhealthy"
	}
	c.mu.Unlock()

	labels := c.getLabels()

	// Delete old metrics with old status if it changed
	if oldStatus != c.currentHealthStatus {
		oldLabels := []string{c.agentName, c.agentVersion, c.peerID, oldStatus}
		c.healthStatus.DeleteLabelValues(oldLabels...)
		c.healthCheckTime.DeleteLabelValues(oldLabels...)
		c.peerCount.DeleteLabelValues(oldLabels...)
		c.connectionCount.DeleteLabelValues(oldLabels...)
		c.mcpToolsCount.DeleteLabelValues(oldLabels...)
		c.mcpRequestsTotal.DeleteLabelValues(oldLabels...)
		c.mcpErrorsTotal.DeleteLabelValues(oldLabels...)
		c.agentInfo.DeleteLabelValues(oldLabels...)
	}

	// Set new values
	if healthy {
		c.healthStatus.WithLabelValues(labels...).Set(1)
	} else {
		c.healthStatus.WithLabelValues(labels...).Set(0)
	}
	c.healthCheckTime.WithLabelValues(labels...).Set(float64(time.Now().Unix()))

	// Update agent info metric with new health status
	c.agentInfo.WithLabelValues(labels...).Set(1)
}

// UpdatePeerCount updates the peer count metric
func (c *MetricsCollector) UpdatePeerCount(count int) {
	labels := c.getLabels()
	c.peerCount.WithLabelValues(labels...).Set(float64(count))
}

// UpdateConnectionCount updates the connection count metric
func (c *MetricsCollector) UpdateConnectionCount(count int) {
	labels := c.getLabels()
	c.connectionCount.WithLabelValues(labels...).Set(float64(count))
}

// UpdateMCPToolsCount updates the MCP tools count metric
func (c *MetricsCollector) UpdateMCPToolsCount(count int) {
	labels := c.getLabels()
	c.mcpToolsCount.WithLabelValues(labels...).Set(float64(count))
}

// IncrementMCPRequests increments the MCP requests counter
func (c *MetricsCollector) IncrementMCPRequests() {
	labels := c.getLabels()
	c.mcpRequestsTotal.WithLabelValues(labels...).Inc()
}

// IncrementMCPErrors increments the MCP errors counter
func (c *MetricsCollector) IncrementMCPErrors() {
	labels := c.getLabels()
	c.mcpErrorsTotal.WithLabelValues(labels...).Inc()
}

// GetRegistry returns the Prometheus registry
func (c *MetricsCollector) GetRegistry() *prometheus.Registry {
	return c.registry
}

// StartRemoteWriter starts the remote write client for pushing metrics
func (c *MetricsCollector) StartRemoteWriter(remoteWriteURL string, pushInterval time.Duration, username, password string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.remoteWriter != nil {
		c.logger.Warn("Remote writer already started")
		return nil
	}

	writer, err := NewRemoteWriter(c.logger, remoteWriteURL, c.registry, pushInterval, username, password)
	if err != nil {
		return err
	}

	c.remoteWriter = writer
	c.remoteWriter.Start()

	c.logger.Infof("Remote writer started, pushing to %s every %s", remoteWriteURL, pushInterval)
	return nil
}

// StopRemoteWriter stops the remote write client
func (c *MetricsCollector) StopRemoteWriter() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.remoteWriter != nil {
		c.remoteWriter.Stop()
		c.remoteWriter = nil
		c.logger.Info("Remote writer stopped")
	}
}
