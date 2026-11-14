package analyzer

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wenfer/qy-plugins/pkg/pluginsdk"
)

// mockHostCtx provides an in-memory HostContext for testing.
type mockHostCtx struct {
	ctx           context.Context
	pluginID      string
	dataDir       string
	storage       *mockStorage
	notifications []pluginsdk.Notification
	notifMu       sync.Mutex
	logger        pluginsdk.Logger
}

func newMockHostCtx(ctx context.Context) *mockHostCtx {
	return &mockHostCtx{
		ctx:      ctx,
		pluginID: "metrics-analyzer",
		dataDir:  "/tmp/metrics-analyzer",
		storage:  newMockStorage(),
		logger:   pluginsdk.NewDefaultLogger("test"),
	}
}

func (m *mockHostCtx) Context() context.Context { return m.ctx }
func (m *mockHostCtx) PluginID() string         { return m.pluginID }
func (m *mockHostCtx) DataDir() string          { return m.dataDir }
func (m *mockHostCtx) Storage() pluginsdk.Storage { return m.storage }
func (m *mockHostCtx) GetServerList(ctx context.Context) ([]pluginsdk.Server, error) {
	return []pluginsdk.Server{{ID: "srv-1", Name: "Server 1"}}, nil
}
func (m *mockHostCtx) GetServer(ctx context.Context, serverID string) (*pluginsdk.Server, error) {
	return &pluginsdk.Server{ID: serverID, Name: "Server " + serverID}, nil
}
func (m *mockHostCtx) SubscribeMetrics(ctx context.Context, opts pluginsdk.SubscribeOptions, handler pluginsdk.MetricsHandler) (pluginsdk.MetricsSubscription, error) {
	return &mockSubscription{serverIDs: opts.ServerIDs}, nil
}
func (m *mockHostCtx) SendNotification(ctx context.Context, n pluginsdk.Notification) error {
	m.notifMu.Lock()
	defer m.notifMu.Unlock()
	m.notifications = append(m.notifications, n)
	return nil
}
func (m *mockHostCtx) Log() pluginsdk.Logger { return m.logger }

func (m *mockHostCtx) Notifications() []pluginsdk.Notification {
	m.notifMu.Lock()
	defer m.notifMu.Unlock()
	res := make([]pluginsdk.Notification, len(m.notifications))
	copy(res, m.notifications)
	return res
}

type mockSubscription struct {
	serverIDs []string
}

func (s *mockSubscription) ServerIDs() []string { return s.serverIDs }
func (s *mockSubscription) Close() error        { return nil }

type mockStorage struct {
	mu   sync.RWMutex
	data map[string]string
}

func newMockStorage() *mockStorage {
	return &mockStorage{data: make(map[string]string)}
}

func (s *mockStorage) Get(ctx context.Context, key string) (string, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.data[key]
	return v, ok, nil
}

func (s *mockStorage) Set(ctx context.Context, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = value
	return nil
}

func (s *mockStorage) Delete(ctx context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, key)
	return nil
}

func (s *mockStorage) List(ctx context.Context, prefix string) (map[string]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	res := make(map[string]string)
	for k, v := range s.data {
		res[k] = v
	}
	return res, nil
}

func (s *mockStorage) Keys(ctx context.Context, prefix string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	keys := make([]string, 0, len(s.data))
	for k := range s.data {
		keys = append(keys, k)
	}
	return keys, nil
}

func (s *mockStorage) GetBytes(ctx context.Context, key string) ([]byte, bool, error) {
	v, ok, err := s.Get(ctx, key)
	return []byte(v), ok, err
}

func (s *mockStorage) SetBytes(ctx context.Context, key string, val []byte) error {
	return s.Set(ctx, key, string(val))
}

func (s *mockStorage) GetJSON(ctx context.Context, key string, v any) (bool, error) {
	_, ok, err := s.Get(ctx, key)
	if err != nil || !ok {
		return ok, err
	}
	return true, nil
}

func (s *mockStorage) SetJSON(ctx context.Context, key string, v any) error {
	return s.Set(ctx, key, "json-data")
}

// 1. TestWorkerPoolThroughput
func TestWorkerPoolThroughput(t *testing.T) {
	cfg := DefaultConfig()
	cfg.NumWorkers = 4
	cfg.QueueCapacity = 20000

	pools := NewMemoryPools()
	var processed atomic.Int64

	handler := func(m *NormalizedMetric) {
		processed.Add(1)
	}

	wp := NewWorkerPool(cfg, pools, handler, nil)
	defer wp.Stop()

	const totalPackets = 10000
	for i := 0; i < totalPackets; i++ {
		job := pools.AcquireJob()
		job.ServerID = fmt.Sprintf("srv-%d", i%10)
		job.BytesIn = 1024
		for !wp.Submit(job) {
			time.Sleep(100 * time.Microsecond)
		}
	}

	// Await completion
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if processed.Load() == totalPackets {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	if processed.Load() != totalPackets {
		t.Fatalf("expected %d packets processed, got %d", totalPackets, processed.Load())
	}

	ingested, proc, dropped := wp.Stats()
	if ingested != totalPackets || proc != totalPackets || dropped != 0 {
		t.Fatalf("unexpected stats: ingested=%d, proc=%d, dropped=%d", ingested, proc, dropped)
	}
}

// 2. TestWorkerPool_SaturationDrop (TestTier2_F8_01 requirement)
func TestWorkerPool_SaturationDrop(t *testing.T) {
	cfg := DefaultConfig()
	cfg.NumWorkers = 0 // No workers running to drain queue
	cfg.QueueCapacity = 1024

	pools := NewMemoryPools()
	wp := &WorkerPool{
		cfg:      cfg,
		pools:    pools,
		jobQueue: make(chan *MetricJob, cfg.QueueCapacity),
	}

	// Ingest burst of 5000 packets
	for i := 0; i < 5000; i++ {
		evt := &pluginsdk.MetricsEvent{ServerID: "srv-1", BytesIn: 1024}
		wp.Ingest(evt, nil)
	}

	expectedDropped := int64(5000 - 1024)
	_, _, dropped := wp.Stats()
	if dropped != expectedDropped {
		t.Fatalf("expected %d dropped packets, got %d", expectedDropped, dropped)
	}
}

// 3. TestBufferRecycler_DeterministicReuse
func TestBufferRecycler_DeterministicReuse(t *testing.T) {
	recycler := NewBufferRecycler(16)

	// Step 1: Acquire and write
	b1 := recycler.Get()
	if cap(b1.Bytes()) < 1024 {
		t.Fatalf("expected capacity >= 1024, got %d", cap(b1.Bytes()))
	}
	b1.WriteString(`{"server_id":"srv-1","ewma_rate":10.5}`)
	ptr1 := fmt.Sprintf("%p", b1)
	recycler.Put(b1)

	// Step 2: Acquire again; must deterministically reuse exact pointer
	b2 := recycler.Get()
	ptr2 := fmt.Sprintf("%p", b2)

	if ptr1 != ptr2 {
		t.Fatalf("expected exact pointer reuse: %s != %s", ptr1, ptr2)
	}
	if b2.Len() != 0 {
		t.Fatalf("expected buffer to be reset, got len %d", b2.Len())
	}
	recycler.Put(b2)
}

// 4. BenchmarkWorkerBuffer_ZeroAlloc (0 B/op requirement)
func BenchmarkWorkerBuffer_ZeroAlloc(b *testing.B) {
	recycler := NewBufferRecycler(64)
	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			buf := recycler.Get()
			buf.WriteString(`{"server_id":"srv-test","bytes_in":2048}`)
			_ = buf.Bytes()
			recycler.Put(buf)
		}
	})
}

// 5. TestEWMACorrectness & Ground Truth Step Response
func TestEWMACorrectness(t *testing.T) {
	calc := NewEWMACalculator(0.2)
	calc.SetBaseline("srv-1", 10.0)

	// Ground truth sequence for step to 60.0 with alpha=0.2:
	// t1: 0.2*60 + 0.8*10 = 20.0
	// t2: 0.2*60 + 0.8*20 = 28.0
	// t3: 0.2*60 + 0.8*28 = 34.4
	// t4: 0.2*60 + 0.8*34.4 = 39.52
	// t5: 0.2*60 + 0.8*39.52 = 43.616
	expected := []float64{20.0, 28.0, 34.4, 39.52, 43.616}
	for step, want := range expected {
		got := calc.UpdateRate("srv-1", 60.0)
		if math.Abs(got-want) > 1e-4 {
			t.Fatalf("step %d: expected %.4f, got %.4f", step+1, want, got)
		}
	}
}

// 6. TestEWMAMissingDecay (TestTier2_F8_05 requirement)
func TestEWMAMissingDecay(t *testing.T) {
	calc := NewEWMACalculator(0.2)
	calc.SetBaseline("srv-idle", 100.0)

	var lastRate float64
	for i := 0; i < 60; i++ {
		lastRate = calc.DecayMissing("srv-idle")
	}

	if lastRate > 0.001 || math.IsNaN(lastRate) || lastRate < 0 {
		t.Fatalf("expected rate to decay below 0.001 without NaN/negative, got %f", lastRate)
	}
}

// 7. TestEWMAExtremeValuesOverflow (TestTier2_F8_03 requirement)
func TestEWMAExtremeValuesOverflow(t *testing.T) {
	calc := NewEWMACalculator(0.2)
	extreme := float64(math.MaxUint64)

	res := calc.UpdateRate("srv-extreme", extreme)
	if math.IsNaN(res) || math.IsInf(res, 0) || res <= 0 {
		t.Fatalf("extreme value calculation produced invalid float: %f", res)
	}
}

// 8. TestSpikeDetection_BoundaryCases (TestTier2_F8_04 requirement)
func TestSpikeDetection_BoundaryCases(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TrafficSpikeMultiplier = 3.0
	cfg.MinTrafficSpikeMBps = 50.0
	cfg.AlertCooldown = 5 * time.Minute

	detector := NewAnomalyDetector(cfg, nil, nil)
	baseline := 10.0

	// 1. rate = 30.0 (3.0 * baseline, but NOT > 50.0 MB/s) -> NOT a spike
	if detector.IsSpike(30.0, baseline) {
		t.Fatalf("30.0 should not be flagged as a spike")
	}

	// 2. rate = 49.9 (below 50.0 threshold) -> NOT a spike
	if detector.IsSpike(49.9, baseline) {
		t.Fatalf("49.9 should not be flagged as a spike")
	}

	// 3. rate = 50.1 (> 3*baseline and > 50.0) -> SPIKE!
	if !detector.IsSpike(50.1, baseline) {
		t.Fatalf("50.1 should be flagged as a spike")
	}

	// 4. Test Cooldown Debounce
	ctx := context.Background()
	mockHost := newMockHostCtx(ctx)
	detWithHost := NewAnomalyDetector(cfg, mockHost, nil)

	state := &ServerState{
		ServerID:              "srv-spike",
		EWMADownloadMB:        10.0,
		CurrentDownloadRateMB: 80.0,
	}

	// First evaluation triggers alert
	firstAlert := detWithHost.Evaluate(ctx, state)
	if !firstAlert {
		t.Fatalf("expected first evaluation to trigger alert")
	}
	time.Sleep(20 * time.Millisecond)
	if len(mockHost.Notifications()) != 1 {
		t.Fatalf("expected 1 notification dispatched, got %d", len(mockHost.Notifications()))
	}

	// Immediate second evaluation is debounced due to 5-minute cooldown
	secondAlert := detWithHost.Evaluate(ctx, state)
	if secondAlert {
		t.Fatalf("expected second evaluation to be suppressed by cooldown")
	}
	if len(mockHost.Notifications()) != 1 {
		t.Fatalf("notification count should remain 1, got %d", len(mockHost.Notifications()))
	}
}

// 9. TestCorruptedPayloadRejection (TestTier2_F8_02 requirement)
func TestCorruptedPayloadRejection(t *testing.T) {
	corrupted := []byte(`{"server_id": "srv-1", "bytes_in": -500}`)
	metric, err := ParseRawPayload(corrupted)
	if err == nil || !errors.Is(err, ErrCorruptedCounter) {
		t.Fatalf("expected ErrCorruptedCounter, got err=%v, metric=%+v", err, metric)
	}
}

// 10. TestStorageFlusher_Persistence (TestTier1_F8_05 & TestTier4_Scenario1)
func TestStorageFlusher_Persistence(t *testing.T) {
	ctx := context.Background()
	mockHost := newMockHostCtx(ctx)
	cfg := DefaultConfig()
	cfg.FlushInterval = 100 * time.Millisecond

	pools := NewMemoryPools()
	calc := NewEWMACalculator(0.2)
	calc.SetBaseline("srv-1", 25.5)

	flusher := NewStorageFlusher(cfg, mockHost, calc, pools, nil)
	flusher.Flush(ctx)
	flusher.Stop()

	// Verify keys persisted in mock storage
	ratesVal, ratesFound, _ := mockHost.storage.Get(ctx, "agg:traffic:srv-1:rates")
	if !ratesFound || ratesVal == "" {
		t.Fatalf("expected agg:traffic:srv-1:rates to be persisted")
	}

	hourlyVal, hourlyFound, _ := mockHost.storage.Get(ctx, "agg:traffic:srv-1:hourly")
	if !hourlyFound || hourlyVal == "" {
		t.Fatalf("expected agg:traffic:srv-1:hourly to be persisted")
	}
}

// 11. TestMetricsAnalyzerPlugin_FullLifecycle
func TestMetricsAnalyzerPlugin_FullLifecycle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mockHost := newMockHostCtx(ctx)
	cfg := DefaultConfig()
	plugin := New(cfg)

	if err := plugin.Start(mockHost); err != nil {
		t.Fatalf("plugin.Start failed: %v", err)
	}

	// Send an event through worker pool
	evt := &pluginsdk.MetricsEvent{
		ServerID: "srv-life",
		BytesIn:  1048576, // 1 MB
	}
	plugin.WorkerPool().Ingest(evt, nil)

	time.Sleep(100 * time.Millisecond)

	// Stop plugin cleanly
	if err := plugin.Stop(); err != nil {
		t.Fatalf("plugin.Stop failed: %v", err)
	}
}
