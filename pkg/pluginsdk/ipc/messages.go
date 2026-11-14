package ipc

// Handshake
type HandshakeReq struct {
	Token    string `json:"token"`
	PluginID string `json:"plugin_id"`
	Version  string `json:"version"`
}

type HandshakeResp struct {
	Success      bool     `json:"success"`
	Error        string   `json:"error,omitempty"`
	HostVersion  string   `json:"host_version"`
	Capabilities []string `json:"capabilities,omitempty"`
}

// Heartbeat
type PingPayload struct {
	Timestamp int64 `json:"timestamp"`
}

type PongPayload struct {
	Timestamp int64 `json:"timestamp"`
}

// Server Query
type GetServerReq struct {
	ServerID string `json:"server_id"`
}

// Metrics
type SubscribeMetricsReq struct {
	ServerIDs  []string `json:"server_ids,omitempty"`
	IntervalMs int      `json:"interval_ms,omitempty"`
}

type SubscribeMetricsResp struct {
	Subscribed bool `json:"subscribed"`
}

type UnsubscribeMetricsReq struct {
	ServerIDs []string `json:"server_ids,omitempty"`
}

type UnsubscribeMetricsResp struct {
	Unsubscribed bool `json:"unsubscribed"`
}

// Storage Key-Value
type StorageGetReq struct {
	Key string `json:"key"`
}

type StorageGetResp struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Found bool   `json:"found"`
}

type StorageSetReq struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type StorageSetResp struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

type StorageDeleteReq struct {
	Key string `json:"key"`
}

type StorageDeleteResp struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

type StorageListReq struct {
	Prefix string `json:"prefix,omitempty"`
}

type StorageListResp struct {
	Keys  []string          `json:"keys,omitempty"`
	Items map[string]string `json:"items,omitempty"`
}

// Notification
type SendNotificationReq struct {
	Title    string         `json:"title"`
	Message  string         `json:"message"`
	ServerID string         `json:"server_id,omitempty"`
	Details  map[string]any `json:"details,omitempty"`
}

type SendNotificationResp struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// Shutdown
type ShutdownReq struct {
	Reason string `json:"reason"`
}

type ShutdownResp struct {
	Ready bool `json:"ready"`
}

// Error
type ErrorPayload struct {
	Code  int    `json:"code,omitempty"`
	Error string `json:"error"`
}
