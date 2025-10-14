package metrics

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/golang/snappy"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/prompb"
	dto "github.com/prometheus/client_model/go"
	"github.com/sirupsen/logrus"
)

// RemoteWriter handles pushing metrics to Prometheus remote write endpoint
type RemoteWriter struct {
	logger         *logrus.Logger
	remoteWriteURL string
	registry       *prometheus.Registry
	pushInterval   time.Duration
	httpClient     *http.Client
	username       string
	password       string

	ctx    context.Context
	cancel context.CancelFunc
}

// NewRemoteWriter creates a new remote writer
func NewRemoteWriter(logger *logrus.Logger, remoteWriteURL string, registry *prometheus.Registry, pushInterval time.Duration, username, password string) (*RemoteWriter, error) {
	if remoteWriteURL == "" {
		return nil, fmt.Errorf("remote write URL cannot be empty")
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &RemoteWriter{
		logger:         logger,
		remoteWriteURL: remoteWriteURL,
		registry:       registry,
		pushInterval:   pushInterval,
		username:       username,
		password:       password,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		ctx:    ctx,
		cancel: cancel,
	}, nil
}

// Start begins pushing metrics to the remote write endpoint
func (w *RemoteWriter) Start() {
	go w.pushLoop()
}

// Stop stops the remote writer
func (w *RemoteWriter) Stop() {
	w.cancel()
}

// pushLoop periodically pushes metrics
func (w *RemoteWriter) pushLoop() {
	ticker := time.NewTicker(w.pushInterval)
	defer ticker.Stop()

	// Push immediately on start
	if err := w.push(); err != nil {
		w.logger.Errorf("Failed to push metrics: %v", err)
	}

	for {
		select {
		case <-ticker.C:
			if err := w.push(); err != nil {
				w.logger.Errorf("Failed to push metrics: %v", err)
			}
		case <-w.ctx.Done():
			w.logger.Info("Stopping remote writer")
			return
		}
	}
}

// push collects metrics and sends them to the remote write endpoint
func (w *RemoteWriter) push() error {
	// Gather metrics from registry
	metricFamilies, err := w.registry.Gather()
	if err != nil {
		return fmt.Errorf("failed to gather metrics: %w", err)
	}

	// Convert to Prometheus remote write format
	timeseries := w.convertToTimeSeries(metricFamilies)
	if len(timeseries) == 0 {
		w.logger.Debug("No metrics to push")
		return nil
	}

	// Create write request
	writeRequest := &prompb.WriteRequest{
		Timeseries: timeseries,
	}

	// Marshal to protobuf
	data, err := writeRequest.Marshal()
	if err != nil {
		return fmt.Errorf("failed to marshal write request: %w", err)
	}

	// Compress with snappy
	compressed := snappy.Encode(nil, data)

	// Create HTTP request
	req, err := http.NewRequestWithContext(w.ctx, "POST", w.remoteWriteURL, bytes.NewReader(compressed))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-protobuf")
	req.Header.Set("Content-Encoding", "snappy")
	req.Header.Set("X-Prometheus-Remote-Write-Version", "0.1.0")
	req.Header.Set("User-Agent", "praxis-agent/1.0")

	// Add basic auth if provided
	if w.username != "" && w.password != "" {
		req.SetBasicAuth(w.username, w.password)
	}

	// Send request
	resp, err := w.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Check response
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("remote write failed with status %d: %s", resp.StatusCode, string(body))
	}

	w.logger.Debugf("Successfully pushed %d time series to remote write endpoint", len(timeseries))
	return nil
}

// convertToTimeSeries converts metric families to Prometheus time series
func (w *RemoteWriter) convertToTimeSeries(metricFamilies []*dto.MetricFamily) []prompb.TimeSeries {
	var timeseries []prompb.TimeSeries
	now := time.Now().UnixMilli()

	for _, mf := range metricFamilies {
		for _, m := range mf.Metric {
			labels := []prompb.Label{
				{
					Name:  "__name__",
					Value: mf.GetName(),
				},
			}

			// Add metric labels
			for _, label := range m.Label {
				labels = append(labels, prompb.Label{
					Name:  label.GetName(),
					Value: label.GetValue(),
				})
			}

			// Extract value based on metric type
			var value float64
			switch mf.GetType() {
			case dto.MetricType_GAUGE:
				if m.Gauge != nil {
					value = m.Gauge.GetValue()
				}
			case dto.MetricType_COUNTER:
				if m.Counter != nil {
					value = m.Counter.GetValue()
				}
			case dto.MetricType_UNTYPED:
				if m.Untyped != nil {
					value = m.Untyped.GetValue()
				}
			case dto.MetricType_SUMMARY:
				// For summaries, we push sum and count
				if m.Summary != nil {
					// Push sum
					sumLabels := append([]prompb.Label{}, labels...)
					sumLabels[0].Value = mf.GetName() + "_sum"
					timeseries = append(timeseries, prompb.TimeSeries{
						Labels: sumLabels,
						Samples: []prompb.Sample{
							{
								Value:     m.Summary.GetSampleSum(),
								Timestamp: now,
							},
						},
					})
					// Push count
					countLabels := append([]prompb.Label{}, labels...)
					countLabels[0].Value = mf.GetName() + "_count"
					timeseries = append(timeseries, prompb.TimeSeries{
						Labels: countLabels,
						Samples: []prompb.Sample{
							{
								Value:     float64(m.Summary.GetSampleCount()),
								Timestamp: now,
							},
						},
					})
					continue
				}
			case dto.MetricType_HISTOGRAM:
				// For histograms, we push sum, count, and buckets
				if m.Histogram != nil {
					// Push sum
					sumLabels := append([]prompb.Label{}, labels...)
					sumLabels[0].Value = mf.GetName() + "_sum"
					timeseries = append(timeseries, prompb.TimeSeries{
						Labels: sumLabels,
						Samples: []prompb.Sample{
							{
								Value:     m.Histogram.GetSampleSum(),
								Timestamp: now,
							},
						},
					})
					// Push count
					countLabels := append([]prompb.Label{}, labels...)
					countLabels[0].Value = mf.GetName() + "_count"
					timeseries = append(timeseries, prompb.TimeSeries{
						Labels: countLabels,
						Samples: []prompb.Sample{
							{
								Value:     float64(m.Histogram.GetSampleCount()),
								Timestamp: now,
							},
						},
					})
					// Push buckets
					for _, bucket := range m.Histogram.Bucket {
						bucketLabels := append([]prompb.Label{}, labels...)
						bucketLabels[0].Value = mf.GetName() + "_bucket"
						bucketLabels = append(bucketLabels, prompb.Label{
							Name:  "le",
							Value: fmt.Sprintf("%g", bucket.GetUpperBound()),
						})
						timeseries = append(timeseries, prompb.TimeSeries{
							Labels: bucketLabels,
							Samples: []prompb.Sample{
								{
									Value:     float64(bucket.GetCumulativeCount()),
									Timestamp: now,
								},
							},
						})
					}
					continue
				}
			default:
				continue
			}

			// Add time series for gauge/counter/untyped
			timeseries = append(timeseries, prompb.TimeSeries{
				Labels: labels,
				Samples: []prompb.Sample{
					{
						Value:     value,
						Timestamp: now,
					},
				},
			})
		}
	}

	return timeseries
}
