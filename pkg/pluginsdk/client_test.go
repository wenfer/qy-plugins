package pluginsdk

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wenfer/qy-plugins/pkg/pluginsdk/ipc"
)

// mockHostController manages an in-memory mock host connection.
type mockHostController struct {
	conn       net.Conn
	writeMu    sync.Mutex
	receivedCh chan *ipc.Frame
	closed     atomic.Bool
}

func newMockHostController(conn net.Conn) *mockHostController {
	h := &mockHostController{
		conn:       conn,
		receivedCh: make(chan *ipc.Frame, 1000),
	}
	go h.readLoop()
	return h
}

func (h *mockHostController) readLoop() {
	for {
		frame, err := ipc.ReadFrame(h.conn)
		if err != nil {
			return
		}
		select {
		case h.receivedCh <- frame:
		default:
		}
	}
}

func (h *mockHostController) Reply(f *ipc.Frame) error {
	h.writeMu.Lock()
	defer h.writeMu.Unlock()
	return ipc.WriteFrame(h.conn, f)
}

func (h *mockHostController) ReplyJSON(msgType ipc.MsgType, seq uint32, payload any) error {
	f, err := ipc.NewJSONFrame(msgType, seq, payload)
	if err != nil {
		return err
	}
	return h.Reply(f)
}

func (h *mockHostController) Close() {
	if !h.closed.Swap(true) {
		_ = h.conn.Close()
	}
}

func TestHandshake_Success(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	host := newMockHostController(serverConn)
	defer host.Close()

	// Server goroutine: handle handshake
	go func() {
		frame := <-host.receivedCh
		if frame.Type != ipc.TypeHandshakeReq {
			t.Errorf("expected TypeHandshakeReq, got 0x%02X", frame.Type)
			return
		}
		var req ipc.HandshakeReq
		_ = frame.UnmarshalPayload(&req)
		if req.Token != "valid-tok" || req.PluginID != "my-plugin" {
			t.Errorf("handshake req mismatch: %+v", req)
			return
		}

		resp := ipc.HandshakeResp{
			Success:      true,
			HostVersion:  "1.0.0",
			Capabilities: []string{"metrics", "storage"},
		}
		_ = host.ReplyJSON(ipc.TypeHandshakeResp, frame.SeqID, resp)
	}()

	mockPlugin := newDummyPlugin()
	opts := ServeOptions{
		PluginID: "my-plugin",
		Token:    "valid-tok",
		Conn:     clientConn,
	}

	// Run serve in goroutine and stop it shortly
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- ServeWithOptions(mockPlugin, opts)
	}()

	select {
	case <-mockPlugin.startedCh:
		// Plugin successfully started!
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for plugin to start")
	}

	// Close server to trigger plugin teardown
	host.Close()

	select {
	case err := <-serveDone:
		if err != nil {
			t.Fatalf("ServeWithOptions error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for serve to complete")
	}
}

func TestHandshake_Rejection(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	host := newMockHostController(serverConn)
	defer host.Close()

	go func() {
		frame := <-host.receivedCh
		resp := ipc.HandshakeResp{
			Success: false,
			Error:   "invalid authentication token",
		}
		_ = host.ReplyJSON(ipc.TypeHandshakeResp, frame.SeqID, resp)
	}()

	mockPlugin := newDummyPlugin()
	opts := ServeOptions{
		PluginID: "my-plugin",
		Token:    "bad-tok",
		Conn:     clientConn,
	}

	err := ServeWithOptions(mockPlugin, opts)
	if err == nil {
		t.Fatalf("expected error on handshake rejection")
	}
	if !errors.Is(err, ErrHandshakeFailed) {
		t.Fatalf("expected ErrHandshakeFailed, got: %v", err)
	}
}

func TestHandshake_ProtocolViolation(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	host := newMockHostController(serverConn)
	defer host.Close()

	go func() {
		frame := <-host.receivedCh
		// Send TypePing instead of TypeHandshakeResp
		_ = host.Reply(ipc.NewFrame(ipc.TypePing, frame.SeqID, nil))
	}()

	mockPlugin := newDummyPlugin()
	opts := ServeOptions{
		PluginID: "my-plugin",
		Token:    "tok",
		Conn:     clientConn,
	}

	err := ServeWithOptions(mockPlugin, opts)
	if err == nil {
		t.Fatalf("expected error on unexpected response type")
	}
	if !errors.Is(err, ErrProtocolViolation) {
		t.Fatalf("expected ErrProtocolViolation, got: %v", err)
	}
}

func TestClient_ConcurrentRPCMultiplexing(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	host := newMockHostController(serverConn)
	defer host.Close()

	client := newClient(clientConn, nil, nil)
	defer client.close()

	const numCalls = 100
	var wg sync.WaitGroup

	// Host responder: echoes KV gets asynchronously in random order
	go func() {
		frames := make([]*ipc.Frame, 0, numCalls)
		for i := 0; i < numCalls; i++ {
			f := <-host.receivedCh
			frames = append(frames, f)
		}
		// Shuffle frames to simulate out-of-order network responses
		rand.Shuffle(len(frames), func(i, j int) {
			frames[i], frames[j] = frames[j], frames[i]
		})
		for _, f := range frames {
			var req ipc.StorageGetReq
			_ = f.UnmarshalPayload(&req)
			resp := ipc.StorageGetResp{
				Key:   req.Key,
				Value: "val-" + req.Key,
				Found: true,
			}
			_ = host.ReplyJSON(ipc.TypeStorageGetResp, f.SeqID, resp)
		}
	}()

	storage := newStorageClient(client)

	// Concurrently issue 100 Storage.Get calls
	for i := 0; i < numCalls; i++ {
		wg.Add(1)
		key := fmt.Sprintf("k-%d", i)
		expectedVal := "val-" + key
		go func(k, want string) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()

			val, found, err := storage.Get(ctx, k)
			if err != nil {
				t.Errorf("get error for %s: %v", k, err)
				return
			}
			if !found || val != want {
				t.Errorf("mismatch for %s: got %s, want %s", k, val, want)
			}
		}(key, expectedVal)
	}

	wg.Wait()
}

func TestClient_ContextCancellation(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	host := newMockHostController(serverConn)
	defer host.Close()

	client := newClient(clientConn, nil, nil)
	defer client.close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := client.Call(ctx, ipc.TypeGetServerListReq, nil, ipc.TypeGetServerListResp)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got: %v", err)
	}

	// Verify pending registry purged the seq channel to prevent leaks
	client.pendingMu.Lock()
	count := len(client.pending)
	client.pendingMu.Unlock()
	if count != 0 {
		t.Fatalf("expected pending map to be empty after cancellation, got %d entries", count)
	}

	// Server now replies late - client must safely discard without panicking
	reqFrame := <-host.receivedCh
	_ = host.ReplyJSON(ipc.TypeGetServerListResp, reqFrame.SeqID, map[string]any{"servers": []any{}})
	time.Sleep(20 * time.Millisecond)
}

func TestClient_ErrorFrameTranslation(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	host := newMockHostController(serverConn)
	defer host.Close()

	client := newClient(clientConn, nil, nil)
	defer client.close()

	go func() {
		f := <-host.receivedCh
		errFrame := ipc.NewErrorFrame(f.SeqID, 404, "server not found")
		_ = host.Reply(errFrame)
	}()

	storage := newStorageClient(client)
	ctx := context.Background()
	_, _, err := storage.Get(ctx, "nonexistent")
	if err == nil {
		t.Fatalf("expected error from 404 response")
	}

	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("expected *RPCError, got %T: %v", err, err)
	}
	if rpcErr.Code != 404 || rpcErr.Message != "server not found" {
		t.Fatalf("unexpected RPCError: %+v", rpcErr)
	}
}

func TestClient_PingPong(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	host := newMockHostController(serverConn)
	defer host.Close()

	client := newClient(clientConn, nil, nil)
	defer client.close()

	// Host sends TypePing
	pingFrame := ipc.NewFrame(ipc.TypePing, 42, []byte("ping-data"))
	_ = host.Reply(pingFrame)

	// Host should receive TypePong with SeqID 42 and identical payload
	select {
	case pong := <-host.receivedCh:
		if pong.Type != ipc.TypePong {
			t.Fatalf("expected TypePong (0x04), got 0x%02X", pong.Type)
		}
		if pong.SeqID != 42 {
			t.Fatalf("expected SeqID 42, got %d", pong.SeqID)
		}
		if string(pong.Payload) != "ping-data" {
			t.Fatalf("expected payload 'ping-data', got '%s'", string(pong.Payload))
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("timed out waiting for pong")
	}
}

func TestClient_MetricsStreamingAndFilter(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	host := newMockHostController(serverConn)
	defer host.Close()

	client := newClient(clientConn, nil, nil)
	defer client.close()

	// Respond to SubscribeMetricsReq
	go func() {
		f := <-host.receivedCh
		if f.Type == ipc.TypeSubscribeMetricsReq {
			_ = host.ReplyJSON(ipc.TypeSubscribeMetricsResp, f.SeqID, ipc.SubscribeMetricsResp{Subscribed: true})
		}
	}()

	var receivedCount atomic.Int64
	handler := MetricsHandlerFunc(func(event *MetricsEvent) {
		if event.Server.ID == "srv-1" {
			receivedCount.Add(1)
		}
	})

	opts := SubscribeOptions{
		ServerIDs:  []string{"srv-1"},
		BufferSize: 100,
	}

	sub, err := client.SubscribeMetrics(context.Background(), opts, handler)
	if err != nil {
		t.Fatalf("SubscribeMetrics failed: %v", err)
	}
	defer sub.Close()

	// Send 5 matching events and 5 non-matching events
	for i := 0; i < 5; i++ {
		evt1 := MetricsEvent{
			Server: Server{ID: "srv-1", Name: "Server 1"},
			Stats:  &ProbeStats{ServerID: "srv-1", CPUPercent: 20.0},
		}
		f1, _ := ipc.NewJSONFrame(ipc.TypeMetricsEvent, uint32(100+i), evt1)
		_ = host.Reply(f1)

		evt2 := MetricsEvent{
			Server: Server{ID: "srv-2", Name: "Server 2"},
			Stats:  &ProbeStats{ServerID: "srv-2", CPUPercent: 50.0},
		}
		f2, _ := ipc.NewJSONFrame(ipc.TypeMetricsEvent, uint32(200+i), evt2)
		_ = host.Reply(f2)
	}

	// Verify only 5 matching events delivered
	time.Sleep(100 * time.Millisecond)
	if receivedCount.Load() != 5 {
		t.Fatalf("expected 5 events received for srv-1, got %d", receivedCount.Load())
	}
}

func TestClient_MetricsSlowConsumerDrop(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	host := newMockHostController(serverConn)
	defer host.Close()

	client := newClient(clientConn, nil, nil)
	defer client.close()

	// Respond to Subscribe
	go func() {
		f := <-host.receivedCh
		_ = host.ReplyJSON(ipc.TypeSubscribeMetricsResp, f.SeqID, ipc.SubscribeMetricsResp{Subscribed: true})
	}()

	// Unresponsive handler that blocks forever
	blockCh := make(chan struct{})
	defer close(blockCh)
	handler := MetricsHandlerFunc(func(event *MetricsEvent) {
		<-blockCh
	})

	sub, err := client.SubscribeMetrics(context.Background(), SubscribeOptions{BufferSize: 2}, handler)
	if err != nil {
		t.Fatalf("subscribe failed: %v", err)
	}
	defer sub.Close()

	// Push 20 events rapidly
	for i := 0; i < 20; i++ {
		evt := MetricsEvent{Server: Server{ID: "srv-1"}}
		f, _ := ipc.NewJSONFrame(ipc.TypeMetricsEvent, uint32(i+1), evt)
		_ = host.Reply(f)
	}

	// Verify client read loop did not deadlock and remains responsive to Ping
	time.Sleep(50 * time.Millisecond)
	_ = host.Reply(ipc.NewFrame(ipc.TypePing, 999, []byte("alive")))

	select {
	case pong := <-host.receivedCh:
		if pong.Type != ipc.TypePong || pong.SeqID != 999 {
			t.Fatalf("expected Pong for seq 999, got 0x%02X seq %d", pong.Type, pong.SeqID)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("read loop stalled due to slow consumer")
	}
}

func TestClient_AbruptSocketSeverance(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	host := newMockHostController(serverConn)

	client := newClient(clientConn, nil, nil)
	defer client.close()

	var wg sync.WaitGroup
	var errCount atomic.Int64

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_, err := client.Call(ctx, ipc.TypeGetServerListReq, nil, ipc.TypeGetServerListResp)
			if err != nil && errors.Is(err, ErrConnectionClosed) {
				errCount.Add(1)
			}
		}()
	}

	// Wait for host to receive the 5 requests so they are registered in pending
	for i := 0; i < 5; i++ {
		select {
		case <-host.receivedCh:
		case <-time.After(1 * time.Second):
			t.Fatalf("timed out waiting for request %d to be received by host", i)
		}
	}

	// Close host connection abruptly without replying
	host.Close()

	wg.Wait()
	if errCount.Load() != 5 {
		t.Fatalf("expected 5 calls to unblock with ErrConnectionClosed, got %d", errCount.Load())
	}
}

type dummyPlugin struct {
	started   atomic.Bool
	stopped   atomic.Bool
	startedCh chan struct{}
}

func newDummyPlugin() *dummyPlugin {
	return &dummyPlugin{
		startedCh: make(chan struct{}),
	}
}

func (d *dummyPlugin) Start(ctx HostContext) error {
	d.started.Store(true)
	close(d.startedCh)
	return nil
}

func (d *dummyPlugin) Stop() error {
	d.stopped.Store(true)
	return nil
}
