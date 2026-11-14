package pluginsdk

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/wenfer/qy-plugins/pkg/pluginsdk/ipc"
)

// ServeOptions allows configuring Serve behavior explicitly (for testing or direct embedding).
type ServeOptions struct {
	PluginID   string
	SocketPath string
	Token      string
	DataDir    string
	Logger     Logger
	Conn       net.Conn // Optional: inject established connection (e.g. net.Pipe in tests)
}

type hostContextImpl struct {
	ctx      context.Context
	pluginID string
	dataDir  string
	client   *Client
	storage  Storage
	logger   Logger
}

func (h *hostContextImpl) Context() context.Context {
	return h.ctx
}

func (h *hostContextImpl) PluginID() string {
	return h.pluginID
}

func (h *hostContextImpl) DataDir() string {
	return h.dataDir
}

func (h *hostContextImpl) Storage() Storage {
	return h.storage
}

func (h *hostContextImpl) GetServerList(ctx context.Context) ([]Server, error) {
	return h.client.GetServerList(ctx)
}

func (h *hostContextImpl) GetServer(ctx context.Context, serverID string) (*Server, error) {
	return h.client.GetServer(ctx, serverID)
}

func (h *hostContextImpl) SubscribeMetrics(ctx context.Context, opts SubscribeOptions, handler MetricsHandler) (MetricsSubscription, error) {
	return h.client.SubscribeMetrics(ctx, opts, handler)
}

func (h *hostContextImpl) SendNotification(ctx context.Context, n Notification) error {
	return h.client.SendNotification(ctx, n)
}

func (h *hostContextImpl) Log() Logger {
	return h.logger
}

// Serve runs the plugin lifecycle with environment variables provided by QY-DASH supervisor.
func Serve(plugin Plugin) error {
	return ServeWithOptions(plugin, ServeOptions{})
}

// ServeWithOptions runs the plugin lifecycle with explicit options.
func ServeWithOptions(plugin Plugin, opts ServeOptions) error {
	if opts.PluginID == "" {
		opts.PluginID = os.Getenv("QY_PLUGIN_ID")
	}
	if opts.SocketPath == "" {
		opts.SocketPath = os.Getenv("QY_PLUGIN_SOCKET")
	}
	if opts.Token == "" {
		opts.Token = os.Getenv("QY_PLUGIN_TOKEN")
	}
	if opts.DataDir == "" {
		opts.DataDir = os.Getenv("QY_PLUGIN_DATA_DIR")
	}
	if opts.Logger == nil {
		opts.Logger = NewDefaultLogger(opts.PluginID)
	}

	if opts.Conn == nil {
		if opts.SocketPath == "" {
			return fmt.Errorf("pluginsdk: missing QY_PLUGIN_SOCKET environment variable")
		}
		if opts.Token == "" {
			return fmt.Errorf("pluginsdk: missing QY_PLUGIN_TOKEN environment variable")
		}
	}
	if opts.PluginID == "" {
		opts.PluginID = "plugin"
	}

	// Step 1: Establish connection
	conn := opts.Conn
	if conn == nil {
		var err error
		conn, err = net.DialTimeout("unix", opts.SocketPath, 5*time.Second)
		if err != nil {
			return fmt.Errorf("pluginsdk: connect to host socket %s failed: %w", opts.SocketPath, err)
		}
	}

	// Step 2: Handshake
	handshakeReq := ipc.HandshakeReq{
		Token:    opts.Token,
		PluginID: opts.PluginID,
		Version:  "1.0.0",
	}
	reqFrame, err := ipc.NewJSONFrame(ipc.TypeHandshakeReq, 1, handshakeReq)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("pluginsdk: marshal handshake frame: %w", err)
	}

	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if err := ipc.WriteFrame(conn, reqFrame); err != nil {
		_ = conn.Close()
		return fmt.Errorf("pluginsdk: send handshake failed: %w", err)
	}

	respFrame, err := ipc.ReadFrame(conn)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("pluginsdk: read handshake response failed: %w", err)
	}
	_ = conn.SetDeadline(time.Time{})

	if respFrame.Type != ipc.TypeHandshakeResp {
		_ = conn.Close()
		return fmt.Errorf("%w: unexpected handshake response type: 0x%02X", ErrProtocolViolation, respFrame.Type)
	}

	var handshakeResp ipc.HandshakeResp
	if err := respFrame.UnmarshalPayload(&handshakeResp); err != nil || !handshakeResp.Success {
		_ = conn.Close()
		return fmt.Errorf("%w: %s", ErrHandshakeFailed, handshakeResp.Error)
	}

	opts.Logger.Infof("Handshake successful with host v%s (capabilities: %v)",
		handshakeResp.HostVersion, handshakeResp.Capabilities)

	// Step 3: Initialize HostContext and Client
	rootCtx, rootCancel := context.WithCancel(context.Background())
	defer rootCancel()

	shutdownTrigger := make(chan struct{}, 1)
	client := newClient(conn, opts.Logger, func() {
		select {
		case shutdownTrigger <- struct{}{}:
		default:
		}
	})
	defer client.close()

	hCtx := &hostContextImpl{
		ctx:      rootCtx,
		pluginID: opts.PluginID,
		dataDir:  opts.DataDir,
		client:   client,
		storage:  newStorageClient(client),
		logger:   opts.Logger,
	}

	// Step 4: Start Plugin
	if err := plugin.Start(hCtx); err != nil {
		return fmt.Errorf("pluginsdk: plugin.Start failed: %w", err)
	}

	// Step 5: Await OS Signals, Host Cooperative Shutdown, or Connection Severance
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	select {
	case sig := <-sigCh:
		opts.Logger.Infof("Received OS signal %s, initiating graceful shutdown", sig)
	case <-shutdownTrigger:
		opts.Logger.Infof("Received host cooperative shutdown, initiating graceful shutdown")
	case <-client.shutdownCh:
		opts.Logger.Warnf("Host connection terminated, initiating graceful shutdown")
	case <-rootCtx.Done():
	}

	// Step 6: Graceful Teardown
	rootCancel()
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer stopCancel()

	stopDone := make(chan error, 1)
	go func() {
		stopDone <- plugin.Stop()
	}()

	select {
	case err := <-stopDone:
		if err != nil {
			opts.Logger.Errorf("plugin.Stop returned error: %v", err)
			return err
		}
		opts.Logger.Infof("plugin.Stop completed cleanly")
	case <-stopCtx.Done():
		opts.Logger.Warnf("plugin.Stop timed out after 3s")
	}

	return nil
}
