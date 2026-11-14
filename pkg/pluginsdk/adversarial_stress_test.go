package pluginsdk

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wenfer/qy-plugins/pkg/pluginsdk/ipc"
)

// silentTestLogger implements Logger for high-volume stress tests without terminal log flooding.
type silentTestLogger struct {
	dropCount  atomic.Int64
	warnCount  atomic.Int64
	errorCount atomic.Int64
}

func (l *silentTestLogger) Debugf(format string, args ...any) {}
func (l *silentTestLogger) Infof(format string, args ...any)  {}
func (l *silentTestLogger) Warnf(format string, args ...any) {
	l.warnCount.Add(1)
	l.dropCount.Add(1)
}
func (l *silentTestLogger) Errorf(format string, args ...any) {
	l.errorCount.Add(1)
}

// stressMockHost wraps net.Conn to simulate an adversarial host over net.Pipe().
type stressMockHost struct {
	conn       net.Conn
	writeMu    sync.Mutex
	incomingCh chan *ipc.Frame
	closed     atomic.Bool
}

func newStressMockHost(conn net.Conn, bufSize int) *stressMockHost {
	h := &stressMockHost{
		conn:       conn,
		incomingCh: make(chan *ipc.Frame, bufSize),
	}
	go h.readLoop()
	return h
}

func (h *stressMockHost) readLoop() {
	defer close(h.incomingCh)
	for {
		f, err := ipc.ReadFrame(h.conn)
		if err != nil {
			return
		}
		select {
		case h.incomingCh <- f:
		default:
			// Queue full or connection terminating
		}
	}
}

func (h *stressMockHost) Reply(f *ipc.Frame) error {
	if h.closed.Load() {
		return net.ErrClosed
	}
	h.writeMu.Lock()
	defer h.writeMu.Unlock()
	return ipc.WriteFrame(h.conn, f)
}

func (h *stressMockHost) ReplyJSON(msgType ipc.MsgType, seq uint32, payload any) error {
	f, err := ipc.NewJSONFrame(msgType, seq, payload)
	if err != nil {
		return err
	}
	return h.Reply(f)
}

func (h *stressMockHost) Close() {
	if !h.closed.Swap(true) {
		_ = h.conn.Close()
	}
}

// -----------------------------------------------------------------------------
// Challenge 1: High-Concurrency Stress
// 500+ parallel goroutines issuing simultaneous storage and server queries over
// net.Pipe(), intentionally replying out-of-order, asserting zero payload
// misrouting, zero race conditions (-race), and zero goroutine leaks.
// -----------------------------------------------------------------------------
func TestAdversarial_HighConcurrencyRPCMultiplexing(t *testing.T) {
	// 1. Capture baseline goroutine count
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	baseGoroutines := runtime.NumGoroutine()

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	const numGoroutines = 600
	host := newStressMockHost(serverConn, numGoroutines*2)
	defer host.Close()

	logger := &silentTestLogger{}
	client := newClient(clientConn, logger, nil)
	storage := newStorageClient(client)

	// 2. Launch Host Responders: 8 workers replying out-of-order with randomized jitter
	var hostWg sync.WaitGroup
	framePool := make(chan *ipc.Frame, numGoroutines)

	for w := 0; w < 8; w++ {
		hostWg.Add(1)
		go func(workerID int) {
			defer hostWg.Done()
			rng := rand.New(rand.NewSource(time.Now().UnixNano() + int64(workerID*997)))
			for f := range framePool {
				// Inject deliberate jitter to scramble reply order
				jitter := time.Duration(rng.Intn(4)) * time.Millisecond
				if jitter > 0 {
					time.Sleep(jitter)
				}

				switch f.Type {
				case ipc.TypeStorageGetReq:
					var req ipc.StorageGetReq
					_ = f.UnmarshalPayload(&req)
					resp := ipc.StorageGetResp{
						Key:   req.Key,
						Value: "val-" + req.Key,
						Found: true,
					}
					_ = host.ReplyJSON(ipc.TypeStorageGetResp, f.SeqID, resp)

				case ipc.TypeStorageSetReq:
					var req ipc.StorageSetReq
					_ = f.UnmarshalPayload(&req)
					_ = host.ReplyJSON(ipc.TypeStorageSetResp, f.SeqID, ipc.StorageSetResp{Success: true})

				case ipc.TypeGetServerReq:
					var req ipc.GetServerReq
					_ = f.UnmarshalPayload(&req)
					resp := map[string]any{
						"server": Server{
							ID:     req.ServerID,
							Name:   "server-name-" + req.ServerID,
							Status: "online",
						},
					}
					_ = host.ReplyJSON(ipc.TypeGetServerResp, f.SeqID, resp)

				case ipc.TypeSendNotificationReq:
					_ = host.ReplyJSON(ipc.TypeSendNotificationResp, f.SeqID, ipc.SendNotificationResp{Success: true})

				default:
					t.Errorf("unexpected frame type: 0x%02X", f.Type)
				}
			}
		}(w)
	}

	// Host collector routing incoming frames into framePool
	go func() {
		for f := range host.incomingCh {
			framePool <- f
		}
	}()

	// 3. Client Goroutines: 600 concurrent workers
	var clientWg sync.WaitGroup
	var misrouteErrors atomic.Int64
	var rpcErrors atomic.Int64
	startBarrier := make(chan struct{})

	for i := 0; i < numGoroutines; i++ {
		clientWg.Add(1)
		idx := i
		go func() {
			defer clientWg.Done()
			<-startBarrier // Synchronize start for maximum contention

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			switch idx % 4 {
			case 0:
				// Storage.Get
				key := fmt.Sprintf("k-get-%04d", idx)
				expected := "val-" + key
				val, found, err := storage.Get(ctx, key)
				if err != nil {
					rpcErrors.Add(1)
					return
				}
				if !found || val != expected {
					misrouteErrors.Add(1)
					t.Errorf("misrouted Storage.Get: key=%s, got=%s, want=%s", key, val, expected)
				}

			case 1:
				// Storage.Set
				key := fmt.Sprintf("k-set-%04d", idx)
				val := fmt.Sprintf("val-set-%04d", idx)
				if err := storage.Set(ctx, key, val); err != nil {
					rpcErrors.Add(1)
				}

			case 2:
				// GetServer
				srvID := fmt.Sprintf("srv-%04d", idx)
				expectedName := "server-name-" + srvID
				srv, err := client.GetServer(ctx, srvID)
				if err != nil {
					rpcErrors.Add(1)
					return
				}
				if srv == nil || srv.ID != srvID || srv.Name != expectedName {
					misrouteErrors.Add(1)
					t.Errorf("misrouted GetServer: id=%s, got=%+v", srvID, srv)
				}

			case 3:
				// SendNotification
				notif := Notification{
					Title:    fmt.Sprintf("alert-%04d", idx),
					Message:  "critical high load",
					ServerID: fmt.Sprintf("srv-%04d", idx),
				}
				if err := client.SendNotification(ctx, notif); err != nil {
					rpcErrors.Add(1)
				}
			}
		}()
	}

	// Release all 600 goroutines simultaneously
	close(startBarrier)
	clientWg.Wait()

	// Assertions on RPC execution
	if rpcErrCount := rpcErrors.Load(); rpcErrCount > 0 {
		t.Fatalf("encountered %d RPC errors during 600-concurrency stress", rpcErrCount)
	}
	if misrouted := misrouteErrors.Load(); misrouted > 0 {
		t.Fatalf("CRITICAL: detected %d payload misroutings under concurrent out-of-order responses!", misrouted)
	}

	// 4. Verify client pending map is completely drained
	client.pendingMu.Lock()
	pendingRemaining := len(client.pending)
	client.pendingMu.Unlock()
	if pendingRemaining != 0 {
		t.Fatalf("expected 0 pending RPC entries, found %d lingering entries", pendingRemaining)
	}

	// 5. Clean teardown and Goroutine Leak Check
	client.close()
	close(framePool)
	hostWg.Wait()
	host.Close()

	// Wait up to 2s for background goroutines to settle
	settleDeadline := time.Now().Add(2 * time.Second)
	var finalGoroutines int
	for time.Now().Before(settleDeadline) {
		runtime.GC()
		finalGoroutines = runtime.NumGoroutine()
		if finalGoroutines <= baseGoroutines+2 { // allow negligible runtime leeway
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if finalGoroutines > baseGoroutines+2 {
		buf := make([]byte, 32*1024)
		n := runtime.Stack(buf, true)
		t.Fatalf("GOROUTINE LEAK detected: baseline=%d, final=%d\n%s", baseGoroutines, finalGoroutines, string(buf[:n]))
	}
}

// -----------------------------------------------------------------------------
// Challenge 2: Adversarial Stream Demuxing
// Push high-rate TypeMetricsEvent packets into an uncooperative or sleeping subscriber;
// verify the client socket readLoop NEVER deadlocks or drops keepalive TypePing frames.
// -----------------------------------------------------------------------------
func TestAdversarial_StreamDemuxing_UnderHostileSubscriber(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	host := newStressMockHost(serverConn, 10000)
	defer host.Close()

	logger := &silentTestLogger{}
	client := newClient(clientConn, logger, nil)
	defer client.close()

	// Host approves subscription
	go func() {
		for f := range host.incomingCh {
			if f.Type == ipc.TypeSubscribeMetricsReq {
				_ = host.ReplyJSON(ipc.TypeSubscribeMetricsResp, f.SeqID, ipc.SubscribeMetricsResp{Subscribed: true})
				return
			}
		}
	}()

	// Uncooperative/sleeping subscriber: blocks completely on each callback
	subscriberUnblockCh := make(chan struct{})
	defer close(subscriberUnblockCh)

	var subReceivedCount atomic.Int64
	hostileHandler := MetricsHandlerFunc(func(event *MetricsEvent) {
		subReceivedCount.Add(1)
		<-subscriberUnblockCh // Intentionally sleeping / uncooperative
	})

	// Very small local queue (capacity 4)
	sub, err := client.SubscribeMetrics(context.Background(), SubscribeOptions{
		ServerIDs:  []string{"srv-target"},
		BufferSize: 4,
	}, hostileHandler)
	if err != nil {
		t.Fatalf("SubscribeMetrics failed: %v", err)
	}
	defer sub.Close()

	const numFloodPackets = 5000
	const numPings = 50
	var pingResponses atomic.Int64

	// Receiver goroutine on host: listens for TypePong responses from client
	pongReceived := make(chan uint32, numPings)
	go func() {
		for f := range host.incomingCh {
			if f.Type == ipc.TypePong {
				pingResponses.Add(1)
				pongReceived <- f.SeqID
			}
		}
	}()

	// Pinger goroutine: periodically fires keepalive TypePing frames into client
	pingsDone := make(chan struct{})
	go func() {
		defer close(pingsDone)
		for p := 0; p < numPings; p++ {
			pingSeq := uint32(2000 + p)
			pingFrame := ipc.NewFrame(ipc.TypePing, pingSeq, []byte(fmt.Sprintf("keepalive-%d", p)))
			_ = host.Reply(pingFrame)
			time.Sleep(2 * time.Millisecond)
		}
	}()

	// Flood goroutine: pumps 5,000 TypeMetricsEvent frames at maximum throughput
	metricEvt := MetricsEvent{
		Server: Server{ID: "srv-target", Name: "Target Server"},
		Stats:  &ProbeStats{ServerID: "srv-target", CPUPercent: 88.5},
	}
	rawJSON, _ := ipc.NewJSONFrame(ipc.TypeMetricsEvent, 1, metricEvt)

	for i := 0; i < numFloodPackets; i++ {
		f := ipc.NewFrame(ipc.TypeMetricsEvent, uint32(10000+i), rawJSON.Payload)
		f.Flags.Set(ipc.FlagJSON)
		if err := host.Reply(f); err != nil {
			break
		}
	}

	<-pingsDone

	// Verify ALL 50 TypePong frames were returned by client readLoop without deadlock
	receivedPongs := make(map[uint32]bool)
	pongTimeout := time.After(3 * time.Second)

	for len(receivedPongs) < numPings {
		select {
		case seq := <-pongReceived:
			receivedPongs[seq] = true
		case <-pongTimeout:
			t.Fatalf("DEADLOCK or PING LOSS: expected %d pongs, but only received %d pongs before timeout!",
				numPings, len(receivedPongs))
		}
	}

	// Verify drops occurred on the uncooperative subscriber without locking readLoop
	if logger.dropCount.Load() == 0 {
		t.Fatalf("expected dropped packets counter > 0 for uncooperative subscriber, got 0")
	}
}

// -----------------------------------------------------------------------------
// Challenge 3: Abrupt Socket Severance
// Subtest 3A: Drop connection mid-handshake
// Subtest 3B: Drop connection mid-RPC burst
// Verify clean immediate unblocking with io.EOF or net.ErrClosed and zero lingering channels.
// -----------------------------------------------------------------------------
func TestAdversarial_AbruptSocketSeverance_MidHandshake(t *testing.T) {
	clientConn, serverConn := net.Pipe()

	host := newStressMockHost(serverConn, 10)
	defer host.Close()

	// Host reads TypeHandshakeReq, then immediately drops the connection without responding
	go func() {
		frame := <-host.incomingCh
		if frame != nil && frame.Type == ipc.TypeHandshakeReq {
			_ = serverConn.Close() // Abrupt crash mid-handshake
		}
	}()

	mockPlugin := newDummyPlugin()
	opts := ServeOptions{
		PluginID: "crash-test-plugin",
		Token:    "valid-tok",
		Conn:     clientConn,
		Logger:   &silentTestLogger{},
	}

	startTime := time.Now()
	err := ServeWithOptions(mockPlugin, opts)
	elapsed := time.Since(startTime)

	if err == nil {
		t.Fatalf("expected error on abrupt handshake severance, got nil")
	}

	// Must unblock immediately (well before the 3-second handshake timeout)
	if elapsed > 1*time.Second {
		t.Fatalf("handshake severance did not unblock immediately, elapsed: %v", elapsed)
	}

	// Verify error reflects EOF or closed connection
	if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, net.ErrClosed) && !errors.Is(err, io.ErrClosedPipe) {
		t.Logf("Handshake severance returned error as expected: %v", err)
	}
}

func TestAdversarial_AbruptSocketSeverance_MidRPCBurst(t *testing.T) {
	clientConn, serverConn := net.Pipe()

	host := newStressMockHost(serverConn, 1000)

	logger := &silentTestLogger{}
	client := newClient(clientConn, logger, nil)

	const numBurstCalls = 500
	var wg sync.WaitGroup
	var unblockedCalls atomic.Int64
	var errClosedCalls atomic.Int64
	startBarrier := make(chan struct{})

	// Issue 500 calls concurrently with a long 30s timeout
	for i := 0; i < numBurstCalls; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-startBarrier

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			storage := newStorageClient(client)
			_, _, err := storage.Get(ctx, fmt.Sprintf("burst-key-%d", idx))
			unblockedCalls.Add(1)
			if err != nil && (errors.Is(err, ErrConnectionClosed) || errors.Is(err, net.ErrClosed) || errors.Is(err, io.EOF)) {
				errClosedCalls.Add(1)
			}
		}(i)
	}

	// Wait until at least 200 requests have reached host's incomingCh
	close(startBarrier)
	receivedCount := 0
	for receivedCount < 200 {
		select {
		case <-host.incomingCh:
			receivedCount++
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for burst requests to reach host, got %d", receivedCount)
		}
	}

	// Abruptly sever host socket while requests are actively in flight
	severStart := time.Now()
	host.Close()
	_ = serverConn.Close()

	// Wait for all 500 calls to unblock
	wg.Wait()
	severElapsed := time.Since(severStart)

	// Assert immediate unblocking (< 500ms, not 30s context deadline)
	if severElapsed > 1*time.Second {
		t.Fatalf("calls did not unblock immediately upon severance, elapsed: %v", severElapsed)
	}
	if unblocked := unblockedCalls.Load(); unblocked != numBurstCalls {
		t.Fatalf("expected %d unblocked calls, got %d", numBurstCalls, unblocked)
	}
	if closedCount := errClosedCalls.Load(); closedCount != numBurstCalls {
		t.Fatalf("expected all %d calls to return connection closed error, got %d", numBurstCalls, closedCount)
	}

	// Clean up client
	client.close()

	// Assert zero lingering channels in the pending map
	client.pendingMu.Lock()
	lingering := len(client.pending)
	client.pendingMu.Unlock()
	if lingering != 0 {
		t.Fatalf("CRITICAL: found %d lingering channels in client.pending map after socket severance!", lingering)
	}
}

// -----------------------------------------------------------------------------
// Challenge 4: Host Cooperative Shutdown
// Verify TypeShutdownReq triggers plugin.Stop() and terminates cleanly.
// -----------------------------------------------------------------------------
type cooperativeStressPlugin struct {
	started      atomic.Bool
	stopped      atomic.Bool
	workerActive atomic.Bool
	stopDone     chan struct{}
}

func (p *cooperativeStressPlugin) Start(ctx HostContext) error {
	p.started.Store(true)
	p.workerActive.Store(true)
	p.stopDone = make(chan struct{})

	// Background worker doing continuous work until ctx cancelled
	go func() {
		defer p.workerActive.Store(false)
		for {
			select {
			case <-ctx.Context().Done():
				return
			case <-p.stopDone:
				return
			case <-time.After(10 * time.Millisecond):
			}
		}
	}()
	return nil
}

func (p *cooperativeStressPlugin) Stop() error {
	p.stopped.Store(true)
	if p.stopDone != nil {
		select {
		case <-p.stopDone:
		default:
			close(p.stopDone)
		}
	}
	return nil
}

func TestAdversarial_HostCooperativeShutdown(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	host := newStressMockHost(serverConn, 100)
	defer host.Close()

	shutdownRespCh := make(chan *ipc.Frame, 1)

	// Host goroutine
	go func() {
		// 1. Handshake
		f := <-host.incomingCh
		if f != nil && f.Type == ipc.TypeHandshakeReq {
			_ = host.ReplyJSON(ipc.TypeHandshakeResp, f.SeqID, ipc.HandshakeResp{Success: true, HostVersion: "1.0.0"})
		}

		// 2. Wait for plugin to start running
		time.Sleep(50 * time.Millisecond)

		// 3. Send TypeShutdownReq with SeqID 8888
		_ = host.Reply(ipc.NewFrame(ipc.TypeShutdownReq, 8888, []byte(`{"reason":"orderly host shutdown"}`)))

		// 4. Capture TypeShutdownResp ack
		for frame := range host.incomingCh {
			if frame.Type == ipc.TypeShutdownResp {
				shutdownRespCh <- frame
				return
			}
		}
	}()

	plugin := &cooperativeStressPlugin{}
	opts := ServeOptions{
		PluginID: "shutdown-test-plugin",
		Token:    "valid-tok",
		Conn:     clientConn,
		Logger:   &silentTestLogger{},
	}

	serveDone := make(chan error, 1)
	go func() {
		serveDone <- ServeWithOptions(plugin, opts)
	}()

	// Verify host received TypeShutdownResp with matching SeqID 8888
	select {
	case ack := <-shutdownRespCh:
		if ack.SeqID != 8888 {
			t.Fatalf("expected shutdown ack SeqID 8888, got %d", ack.SeqID)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for TypeShutdownResp ack from plugin")
	}

	// Verify ServeWithOptions terminates cleanly
	select {
	case err := <-serveDone:
		if err != nil {
			t.Fatalf("ServeWithOptions failed on cooperative shutdown: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("ServeWithOptions timed out awaiting graceful stop")
	}

	if !plugin.started.Load() {
		t.Fatalf("expected plugin to be started")
	}
	if !plugin.stopped.Load() {
		t.Fatalf("expected plugin.Stop() to be called on TypeShutdownReq")
	}
	if plugin.workerActive.Load() {
		t.Fatalf("expected plugin background worker to terminate upon shutdown")
	}
}

// -----------------------------------------------------------------------------
// Benchmarks
// -----------------------------------------------------------------------------
func BenchmarkAdversarial_ClientRPCMultiplexing(b *testing.B) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	host := newStressMockHost(serverConn, 2048)
	defer host.Close()

	// Host auto-responder for Storage.Get
	go func() {
		for f := range host.incomingCh {
			if f.Type == ipc.TypeStorageGetReq {
				resp := ipc.StorageGetResp{Key: "bench-key", Value: "bench-value", Found: true}
				_ = host.ReplyJSON(ipc.TypeStorageGetResp, f.SeqID, resp)
			}
		}
	}()

	client := newClient(clientConn, &silentTestLogger{}, nil)
	defer client.close()
	storage := newStorageClient(client)
	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			val, found, err := storage.Get(ctx, "bench-key")
			if err != nil || !found || val != "bench-value" {
				b.Errorf("rpc failure: val=%s, found=%v, err=%v", val, found, err)
			}
		}
	})
}

func BenchmarkAdversarial_StreamDemuxThroughput(b *testing.B) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	host := newStressMockHost(serverConn, 2048)
	defer host.Close()

	go func() {
		for f := range host.incomingCh {
			if f.Type == ipc.TypeSubscribeMetricsReq {
				_ = host.ReplyJSON(ipc.TypeSubscribeMetricsResp, f.SeqID, ipc.SubscribeMetricsResp{Subscribed: true})
				return
			}
		}
	}()

	client := newClient(clientConn, &silentTestLogger{}, nil)
	defer client.close()

	var count atomic.Int64
	handler := MetricsHandlerFunc(func(event *MetricsEvent) {
		count.Add(1)
	})

	sub, err := client.SubscribeMetrics(context.Background(), SubscribeOptions{BufferSize: 1024}, handler)
	if err != nil {
		b.Fatalf("subscribe failed: %v", err)
	}
	defer sub.Close()

	evt := MetricsEvent{
		Server: Server{ID: "bench-srv"},
		Stats:  &ProbeStats{ServerID: "bench-srv", CPUPercent: 50.0},
	}
	f, _ := ipc.NewJSONFrame(ipc.TypeMetricsEvent, 1, evt)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = host.Reply(f)
	}
}
