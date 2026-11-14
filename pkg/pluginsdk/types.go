package pluginsdk

import (
	"errors"
	"fmt"
	"time"
)

// Common SDK error definitions.
var (
	ErrNotConnected       = errors.New("pluginsdk: not connected to host")
	ErrConnectionClosed   = errors.New("pluginsdk: host connection closed")
	ErrTimeout            = errors.New("pluginsdk: request timed out")
	ErrSubscriptionClosed = errors.New("pluginsdk: subscription closed")
	ErrHandshakeFailed    = errors.New("pluginsdk: handshake failed")
	ErrProtocolViolation  = errors.New("pluginsdk: protocol violation")
)

// RPCError represents an application-level error returned by the host.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"error"`
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("host rpc error (code %d): %s", e.Code, e.Message)
}

// SubscribeOptions configures real-time metrics streaming subscriptions.
type SubscribeOptions struct {
	ServerIDs  []string `json:"server_ids,omitempty"`  // Filter to specific servers; empty = all servers
	IntervalMs int      `json:"interval_ms,omitempty"` // Requested telemetry polling interval hint
	BufferSize int      `json:"-"`                     // Local worker queue size (default 256)
}

// Notification defines an alert dispatched to host notification channels.
type Notification struct {
	Title    string         `json:"title"`
	Message  string         `json:"message"`
	ServerID string         `json:"server_id,omitempty"`
	Details  map[string]any `json:"details,omitempty"`
}

// MetricsEvent represents a real-time probe telemetry broadcast from the host.
type MetricsEvent struct {
	Server     Server      `json:"server"`
	Stats      *ProbeStats `json:"stats,omitempty"`
	Node       *XrayNode   `json:"node,omitempty"`
	Nodes      []*XrayNode `json:"nodes,omitempty"`
	ReceivedAt time.Time   `json:"-"`

	// Fallback fields for synthetic test events
	ServerID string  `json:"server_id,omitempty"`
	CPU      float64 `json:"cpu,omitempty"`
	BytesIn  int64   `json:"bytes_in,omitempty"`
	BytesOut int64   `json:"bytes_out,omitempty"`
	Raw      []byte  `json:"-"`
}

// Server represents monitored server metadata.
type Server struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Host          string `json:"host"`
	SSHPort       int    `json:"ssh_port"`
	AuthType      string `json:"auth_type"`
	SSHUser       string `json:"ssh_user"`
	HasCredential bool   `json:"has_credential"`
	NetworkType   string `json:"network_type"`
	CountryCode   string `json:"country_code"`
	CountryName   string `json:"country_name"`
	Status        string `json:"status"`
	PingLatencyMs int64  `json:"ping_latency_ms"`
}

// ProbeStats represents detailed server telemetry metrics.
type ProbeStats struct {
	ServerID         string    `json:"server_id"`
	Timestamp        time.Time `json:"timestamp"`
	HostName         string    `json:"hostname,omitempty"`
	OS               string    `json:"os"`
	Kernel           string    `json:"kernel"`
	Arch             string    `json:"arch"`
	CPUModel         string    `json:"cpu_model"`
	CPUCores         int       `json:"cpu_cores"`
	UptimeSeconds    uint64    `json:"uptime_seconds"`
	CPUPercent       float64   `json:"cpu_percent"`
	MemTotal         uint64    `json:"mem_total"`
	MemUsed          uint64    `json:"mem_used"`
	MemPercent       float64   `json:"mem_percent"`
	SwapTotal        uint64    `json:"swap_total"`
	SwapUsed         uint64    `json:"swap_used"`
	DiskTotal        uint64    `json:"disk_total"`
	DiskUsed         uint64    `json:"disk_used"`
	DiskPercent      float64   `json:"disk_percent"`
	Load1            float64   `json:"load_1"`
	Load5            float64   `json:"load_5"`
	Load15           float64   `json:"load_15"`
	NetUploadSpeed   uint64    `json:"net_upload_speed"`
	NetDownloadSpeed uint64    `json:"net_download_speed"`
	NetTotalSent     uint64    `json:"net_total_sent"`
	NetTotalRecv     uint64    `json:"net_total_recv"`
	MonthTotalSent   uint64    `json:"month_total_sent"`
	MonthTotalRecv   uint64    `json:"month_total_recv"`
	TCPConnCount     int       `json:"tcp_conn_count"`
	PingLatencyMs    int64     `json:"ping_latency_ms"`
}

// XrayNode represents proxy core status.
type XrayNode struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
	Status   string `json:"status"`
}

// ServerWithStats embeds Server with its latest real-time ProbeStats and XrayNode info.
type ServerWithStats struct {
	Server Server      `json:"server"`
	Stats  *ProbeStats `json:"stats,omitempty"`
	Node   *XrayNode   `json:"node,omitempty"`
	Nodes  []*XrayNode `json:"nodes,omitempty"`
}
