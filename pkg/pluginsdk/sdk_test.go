package pluginsdk

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wenfer/qy-plugins/pkg/pluginsdk/ipc"
)

type sampleData struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func TestStorageClient_Methods(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	host := newMockHostController(serverConn)
	defer host.Close()

	client := newClient(clientConn, nil, nil)
	defer client.close()

	storage := newStorageClient(client)
	ctx := context.Background()

	// Server handler
	go func() {
		for frame := range host.receivedCh {
			switch frame.Type {
			case ipc.TypeStorageSetReq:
				var req ipc.StorageSetReq
				_ = frame.UnmarshalPayload(&req)
				_ = host.ReplyJSON(ipc.TypeStorageSetResp, frame.SeqID, ipc.StorageSetResp{Success: true})

			case ipc.TypeStorageGetReq:
				var req ipc.StorageGetReq
				_ = frame.UnmarshalPayload(&req)
				if req.Key == "missing" {
					_ = host.ReplyJSON(ipc.TypeStorageGetResp, frame.SeqID, ipc.StorageGetResp{Key: req.Key, Found: false})
				} else {
					_ = host.ReplyJSON(ipc.TypeStorageGetResp, frame.SeqID, ipc.StorageGetResp{
						Key:   req.Key,
						Value: `{"name":"alice","count":42}`,
						Found: true,
					})
				}

			case ipc.TypeStorageDeleteReq:
				_ = host.ReplyJSON(ipc.TypeStorageDeleteResp, frame.SeqID, ipc.StorageDeleteResp{Success: true})

			case ipc.TypeStorageListReq:
				_ = host.ReplyJSON(ipc.TypeStorageListResp, frame.SeqID, ipc.StorageListResp{
					Keys:  []string{"k1", "k2"},
					Items: map[string]string{"k1": "v1", "k2": "v2"},
				})
			}
		}
	}()

	// 1. Test Set and Get
	if err := storage.Set(ctx, "user:1", "data"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	val, found, err := storage.Get(ctx, "user:1")
	if err != nil || !found || val == "" {
		t.Fatalf("Get failed: val=%s, found=%v, err=%v", val, found, err)
	}

	// 2. Test Get on missing key
	_, foundMissing, err := storage.Get(ctx, "missing")
	if err != nil || foundMissing {
		t.Fatalf("expected missing key not found")
	}

	// 3. Test SetBytes and GetBytes
	if err := storage.SetBytes(ctx, "bytes:1", []byte("raw-bytes")); err != nil {
		t.Fatalf("SetBytes failed: %v", err)
	}
	bytesVal, found, err := storage.GetBytes(ctx, "bytes:1")
	if err != nil || !found || string(bytesVal) != `{"name":"alice","count":42}` {
		t.Fatalf("GetBytes unexpected result: %s", string(bytesVal))
	}

	// 4. Test SetJSON and GetJSON
	inData := sampleData{Name: "alice", Count: 42}
	if err := storage.SetJSON(ctx, "json:1", inData); err != nil {
		t.Fatalf("SetJSON failed: %v", err)
	}
	var outData sampleData
	foundJSON, err := storage.GetJSON(ctx, "json:1", &outData)
	if err != nil || !foundJSON || outData.Name != "alice" || outData.Count != 42 {
		t.Fatalf("GetJSON mismatch: %+v, found=%v, err=%v", outData, foundJSON, err)
	}

	// 5. Test Delete
	if err := storage.Delete(ctx, "del:1"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// 6. Test List and Keys
	items, err := storage.List(ctx, "prefix:")
	if err != nil || len(items) != 2 || items["k1"] != "v1" {
		t.Fatalf("List unexpected: %+v, err=%v", items, err)
	}

	keys, err := storage.Keys(ctx, "prefix:")
	if err != nil || len(keys) != 2 || keys[0] != "k1" {
		t.Fatalf("Keys unexpected: %+v, err=%v", keys, err)
	}
}

func TestHostContext_QueriesAndNotifications(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	host := newMockHostController(serverConn)
	defer host.Close()

	client := newClient(clientConn, nil, nil)
	defer client.close()

	ctx := context.Background()

	go func() {
		for frame := range host.receivedCh {
			switch frame.Type {
			case ipc.TypeGetServerListReq:
				servers := []Server{
					{ID: "srv-1", Name: "Tokyo-1", Status: "online"},
					{ID: "srv-2", Name: "US-West", Status: "online"},
				}
				_ = host.ReplyJSON(ipc.TypeGetServerListResp, frame.SeqID, map[string]any{"servers": servers})

			case ipc.TypeGetServerReq:
				var req ipc.GetServerReq
				_ = frame.UnmarshalPayload(&req)
				s := Server{ID: req.ServerID, Name: "Single-Server", Status: "online"}
				_ = host.ReplyJSON(ipc.TypeGetServerResp, frame.SeqID, map[string]any{"server": s})

			case ipc.TypeSendNotificationReq:
				_ = host.ReplyJSON(ipc.TypeSendNotificationResp, frame.SeqID, ipc.SendNotificationResp{Success: true})
			}
		}
	}()

	// Query Server List
	servers, err := client.GetServerList(ctx)
	if err != nil || len(servers) != 2 || servers[0].ID != "srv-1" {
		t.Fatalf("GetServerList error: %v, servers=%+v", err, servers)
	}

	// Query Single Server
	server, err := client.GetServer(ctx, "srv-target")
	if err != nil || server == nil || server.ID != "srv-target" {
		t.Fatalf("GetServer error: %v, server=%+v", err, server)
	}

	// Send Notification
	notif := Notification{
		Title:    "Disk Alert",
		Message:  "Root partition 95% full",
		ServerID: "srv-target",
		Details:  map[string]any{"mount": "/"},
	}
	if err := client.SendNotification(ctx, notif); err != nil {
		t.Fatalf("SendNotification error: %v", err)
	}
}

type fullLifecyclePlugin struct {
	started atomic.Bool
	stopped atomic.Bool
	hCtx    HostContext
}

func (p *fullLifecyclePlugin) Start(ctx HostContext) error {
	p.hCtx = ctx
	p.started.Store(true)
	return nil
}

func (p *fullLifecyclePlugin) Stop() error {
	p.stopped.Store(true)
	return nil
}

func TestServeWithOptions_GracefulShutdownByHost(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	host := newMockHostController(serverConn)
	defer host.Close()

	go func() {
		// 1. Handshake
		f := <-host.receivedCh
		_ = host.ReplyJSON(ipc.TypeHandshakeResp, f.SeqID, ipc.HandshakeResp{Success: true, HostVersion: "1.0.0"})

		// Wait for plugin start
		time.Sleep(50 * time.Millisecond)

		// 2. Host sends ShutdownReq
		_ = host.Reply(ipc.NewFrame(ipc.TypeShutdownReq, 888, []byte(`{"reason":"host stopping"}`)))

		// 3. Await ShutdownResp ack
		resp := <-host.receivedCh
		if resp.Type != ipc.TypeShutdownResp {
			t.Errorf("expected ShutdownResp ack, got 0x%02X", resp.Type)
		}
	}()

	plugin := &fullLifecyclePlugin{}
	opts := ServeOptions{
		PluginID: "test-plugin",
		Token:    "test-tok",
		DataDir:  "/tmp/test-data",
		Conn:     clientConn,
	}

	err := ServeWithOptions(plugin, opts)
	if err != nil {
		t.Fatalf("ServeWithOptions error: %v", err)
	}

	if !plugin.started.Load() {
		t.Fatalf("expected plugin.Start to be called")
	}
	if !plugin.stopped.Load() {
		t.Fatalf("expected plugin.Stop to be called on host shutdown")
	}
	if plugin.hCtx.PluginID() != "test-plugin" || plugin.hCtx.DataDir() != "/tmp/test-data" {
		t.Fatalf("HostContext properties mismatch: %+v", plugin.hCtx)
	}
}

func TestDefaultLogger(t *testing.T) {
	logger := NewDefaultLogger("test-logger")
	// Smoke test logger calls without panicking
	logger.Debugf("debug %s", "msg")
	logger.Infof("info %s", "msg")
	logger.Warnf("warn %s", "msg")
	logger.Errorf("error %s", "msg")
}

func TestSDKGuide_ContractTypes(t *testing.T) {
	// 1. Notification contract matching SDK Guide
	notification := Notification{
		Title:    "Traffic Spike Detected",
		Message:  "Server [us-east-node] exceeded EWMA threshold: 142.50 MB/s",
		ServerID: "us-east-node",
		Details: map[string]any{
			"level":    "warning",
			"category": "telemetry",
			"tags":     []string{"bandwidth", "anomaly", "us-east"},
		},
	}
	if notification.Title == "" || notification.ServerID != "us-east-node" {
		t.Fatalf("unexpected notification: %+v", notification)
	}
	if lvl, ok := notification.Details["level"].(string); !ok || lvl != "warning" {
		t.Fatalf("unexpected notification details: %+v", notification.Details)
	}

	// 2. Server contract matching SDK Guide
	server := Server{
		ID:            "srv-us-east-01",
		Name:          "US East Node",
		Host:          "198.51.100.10",
		Status:        "online",
		PingLatencyMs: 42,
	}
	if server.Status != "online" || server.PingLatencyMs != 42 {
		t.Fatalf("unexpected server fields: %+v", server)
	}

	// 3. MetricsEvent and ProbeStats telemetry matching SDK Guide
	event := MetricsEvent{
		Server: server,
		Stats: &ProbeStats{
			ServerID:         server.ID,
			Timestamp:        time.Now(),
			CPUPercent:       18.5,
			MemPercent:       45.2,
			NetDownloadSpeed: 52428800, // 50 MB/s
			NetUploadSpeed:   10485760, // 10 MB/s
			NetTotalRecv:     1073741824,
			NetTotalSent:     536870912,
			TCPConnCount:     120,
		},
	}
	if event.Stats.NetDownloadSpeed != 52428800 || event.Stats.CPUPercent != 18.5 {
		t.Fatalf("unexpected stats telemetry: %+v", event.Stats)
	}
}
