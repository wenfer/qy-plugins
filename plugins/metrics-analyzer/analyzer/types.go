package analyzer

import (
	"encoding/json"
	"errors"
	"runtime"
	"time"

	"github.com/wenfer/qy-plugins/pkg/pluginsdk"
)

// ErrCorruptedCounter indicates an invalid negative telemetry metric counter.
var ErrCorruptedCounter = errors.New("analyzer: rejected corrupted negative counter")

// Config configures analyzer concurrency, thresholds, and persistence.
type Config struct {
	NumWorkers             int           `json:"num_workers"`
	QueueCapacity          int           `json:"queue_capacity"`
	EWMAAlpha              float64       `json:"ewma_alpha"`
	TrafficSpikeMultiplier float64       `json:"traffic_spike_multiplier"`
	MinTrafficSpikeMBps    float64       `json:"min_traffic_spike_mbps"`
	CPUThresholdPercent    float64       `json:"cpu_threshold_percent"`
	MemThresholdPercent    float64       `json:"mem_threshold_percent"`
	AlertCooldown          time.Duration `json:"alert_cooldown"`
	FlushInterval          time.Duration `json:"flush_interval"`
}

// DefaultConfig returns production default parameters matching E2E test specs.
func DefaultConfig() Config {
	workers := runtime.NumCPU() * 2
	if workers < 4 {
		workers = 4
	}
	return Config{
		NumWorkers:             workers,
		QueueCapacity:          1024, // Bounded queue capacity for TestTier2_F8_01
		EWMAAlpha:              0.2,  // Default alpha for TestTier1_F8_04
		TrafficSpikeMultiplier: 3.0,  // Spike rule: rate > 3.0 * EWMA (TestTier2_F8_04)
		MinTrafficSpikeMBps:    50.0, // Spike rule: rate > 50.0 MB/s (TestTier2_F8_04)
		CPUThresholdPercent:    90.0,
		MemThresholdPercent:    92.0,
		AlertCooldown:          5 * time.Minute,
		FlushInterval:          10 * time.Second, // Storage flush interval for TestTier1_F8_05
	}
}

// NormalizedMetric represents a sanitized telemetry packet ready for analytical calculations.
type NormalizedMetric struct {
	ServerID       string    `json:"server_id"`
	Timestamp      time.Time `json:"timestamp"`
	CPUPercent     float64   `json:"cpu_percent"`
	MemPercent     float64   `json:"mem_percent"`
	DownloadRateMB float64   `json:"download_rate_mb"`
	UploadRateMB   float64   `json:"upload_rate_mb"`
	TotalRecvBytes uint64    `json:"total_recv_bytes"`
	TotalSentBytes uint64    `json:"total_sent_bytes"`
}

// TrafficSnapshot represents the document persisted in isolated KV storage.
// Matches keys: agg:traffic:<server_id>:rates and agg:traffic:<server_id>:hourly.
type TrafficSnapshot struct {
	ServerID       string  `json:"server_id"`
	RateIn         int64   `json:"rate_in"`          // Bytes/sec
	RateOut        int64   `json:"rate_out"`         // Bytes/sec
	EWMARate       float64 `json:"ewma_rate"`        // MB/s (for E2E compatibility)
	EWMARateInMB   float64 `json:"ewma_rate_in_mb"`  // MB/s
	EWMARateOutMB  float64 `json:"ewma_rate_out_mb"` // MB/s
	EWMACPUPercent float64 `json:"ewma_cpu_percent"`
	Timestamp      int64   `json:"timestamp"`
}

// MetricJob wraps raw data or structured events for worker pool queues.
type MetricJob struct {
	Event      *pluginsdk.MetricsEvent
	RawData    []byte
	ReceivedAt time.Time
	ServerID   string
	BytesIn    int64
	BytesOut   int64
	OnComplete func()
}

// Reset clears the job for sync.Pool reuse.
func (j *MetricJob) Reset() {
	j.Event = nil
	j.RawData = nil
	j.ReceivedAt = time.Time{}
	j.ServerID = ""
	j.BytesIn = 0
	j.BytesOut = 0
	j.OnComplete = nil
}

// ExtractNormalized parses both authentic ServerWithStats events and synthetic test harness JSON.
func ExtractNormalized(event *pluginsdk.MetricsEvent) (*NormalizedMetric, bool) {
	if event == nil {
		return nil, false
	}

	norm := &NormalizedMetric{
		Timestamp: time.Now(),
	}

	// 1. Authentic ProbeStats telemetry
	if event.Stats != nil && event.Stats.ServerID != "" {
		norm.ServerID = event.Stats.ServerID
		if !event.Stats.Timestamp.IsZero() {
			norm.Timestamp = event.Stats.Timestamp
		}
		norm.CPUPercent = event.Stats.CPUPercent
		norm.MemPercent = event.Stats.MemPercent
		norm.TotalRecvBytes = event.Stats.NetTotalRecv
		norm.TotalSentBytes = event.Stats.NetTotalSent

		if event.Stats.NetDownloadSpeed > 0 {
			norm.DownloadRateMB = float64(event.Stats.NetDownloadSpeed) / (1024.0 * 1024.0)
		}
		if event.Stats.NetUploadSpeed > 0 {
			norm.UploadRateMB = float64(event.Stats.NetUploadSpeed) / (1024.0 * 1024.0)
		}
		return norm, true
	}

	// 2. Synthetic test JSON fields at top level
	if event.ServerID != "" {
		if event.BytesIn < 0 || event.BytesOut < 0 || event.CPU < 0 {
			return nil, false
		}
		norm.ServerID = event.ServerID
		norm.CPUPercent = event.CPU
		if event.BytesIn > 0 {
			norm.DownloadRateMB = float64(event.BytesIn) / (1024.0 * 1024.0)
			norm.TotalRecvBytes = uint64(event.BytesIn)
		}
		if event.BytesOut > 0 {
			norm.UploadRateMB = float64(event.BytesOut) / (1024.0 * 1024.0)
			norm.TotalSentBytes = uint64(event.BytesOut)
		}
		return norm, true
	}

	if event.Server.ID != "" {
		norm.ServerID = event.Server.ID
		return norm, true
	}

	return nil, false
}

// ParseRawPayload parses and validates raw JSON metric bytes (e.g. {"server_id":"srv-1","cpu":45.5,"bytes_in":1048576}).
func ParseRawPayload(data []byte) (*NormalizedMetric, error) {
	var raw struct {
		ServerID string  `json:"server_id"`
		CPU      float64 `json:"cpu"`
		BytesIn  int64   `json:"bytes_in"`
		BytesOut int64   `json:"bytes_out"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	if raw.ServerID == "" {
		return nil, nil
	}

	// Reject negative or corrupted counters (TestTier2_F8_02)
	if raw.BytesIn < 0 || raw.BytesOut < 0 || raw.CPU < 0 {
		return nil, ErrCorruptedCounter
	}

	return &NormalizedMetric{
		ServerID:       raw.ServerID,
		Timestamp:      time.Now(),
		CPUPercent:     raw.CPU,
		DownloadRateMB: float64(raw.BytesIn) / (1024.0 * 1024.0),
		UploadRateMB:   float64(raw.BytesOut) / (1024.0 * 1024.0),
		TotalRecvBytes: uint64(raw.BytesIn),
		TotalSentBytes: uint64(raw.BytesOut),
	}, nil
}
