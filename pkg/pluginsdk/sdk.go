package pluginsdk

import (
	"context"
)

// Plugin defines the lifecycle interface every QY-DASH Go plugin must implement.
type Plugin interface {
	// Start is invoked once the UDS socket connection and handshake succeed.
	// The plugin should spawn its background workers and return promptly.
	Start(ctx HostContext) error

	// Stop is invoked during graceful shutdown (SIGTERM, SIGINT, or host ShutdownReq).
	// The plugin should flush buffers, stop worker pools, and release resources.
	Stop() error
}

// HostContext exposes host services and environment information to the plugin.
type HostContext interface {
	// Context returns the root lifecycle context, canceled when the plugin is stopping.
	Context() context.Context

	// PluginID returns the unique identifier of this plugin (from QY_PLUGIN_ID).
	PluginID() string

	// DataDir returns the dedicated persistent directory for this plugin (from QY_PLUGIN_DATA_DIR).
	DataDir() string

	// Storage returns the isolated Key-Value storage client.
	Storage() Storage

	// GetServerList queries the host for all monitored servers.
	GetServerList(ctx context.Context) ([]Server, error)

	// GetServer queries the host for details of a specific server.
	GetServer(ctx context.Context, serverID string) (*Server, error)

	// SubscribeMetrics initiates real-time streaming of probe metrics from the host.
	SubscribeMetrics(ctx context.Context, opts SubscribeOptions, handler MetricsHandler) (MetricsSubscription, error)

	// SendNotification dispatches an alert notification through host notification channels.
	SendNotification(ctx context.Context, n Notification) error

	// Log returns a scoped logger for this plugin.
	Log() Logger
}

// Storage provides isolated Key-Value storage operations scoped to this plugin ID.
type Storage interface {
	// Raw string operations matching host IPC
	Get(ctx context.Context, key string) (value string, found bool, err error)
	Set(ctx context.Context, key, value string) error
	Delete(ctx context.Context, key string) error
	List(ctx context.Context, prefix string) (map[string]string, error)
	Keys(ctx context.Context, prefix string) ([]string, error)

	// High-level convenience helpers
	GetBytes(ctx context.Context, key string) ([]byte, bool, error)
	SetBytes(ctx context.Context, key string, value []byte) error
	GetJSON(ctx context.Context, key string, v any) (bool, error)
	SetJSON(ctx context.Context, key string, v any) error
}

// MetricsSubscription represents an active metrics stream.
type MetricsSubscription interface {
	// ServerIDs returns the subscribed server IDs filter.
	ServerIDs() []string

	// Close cancels the subscription on the host and releases local resources.
	Close() error
}

// MetricsHandler handles incoming metrics broadcast events.
type MetricsHandler interface {
	HandleMetrics(event *MetricsEvent)
}

// MetricsHandlerFunc is an adapter allowing bare functions to act as MetricsHandler.
type MetricsHandlerFunc func(event *MetricsEvent)

func (f MetricsHandlerFunc) HandleMetrics(event *MetricsEvent) {
	f(event)
}

// Logger defines logging capabilities for plugins.
type Logger interface {
	Debugf(format string, args ...any)
	Infof(format string, args ...any)
	Warnf(format string, args ...any)
	Errorf(format string, args ...any)
}
