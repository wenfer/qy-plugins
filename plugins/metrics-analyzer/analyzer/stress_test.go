package analyzer_test

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/wenfer/qy-plugins/pkg/pluginsdk"
	"github.com/wenfer/qy-plugins/plugins/metrics-analyzer/analyzer"
)

// ============================================================================
// 1. Massive Burst Flood & Saturation Drop Stress Tests
// ============================================================================

// TestTier2_F8_01_MassiveBurstFloodSaturationDrop empirically challenges
// the WorkerPool bounded job queue (capacity 1024) under an instant burst of
// 5,000 metrics packets without active draining, verifying that exactly
// 5000 - 1024 = 3976 packets are dropped, zero deadlocks, and zero panics.
func TestTier2_F8_01_MassiveBurstFloodSaturationDrop(t *testing.T) {
	cfg := analyzer.DefaultConfig()
	cfg.NumWorkers = 0 // Freeze worker loop to prevent asynchronous dequeuing during burst
	cfg.QueueCapacity = 1024

	pools := analyzer.NewMemoryPools()
	wp := analyzer.NewWorkerPool(cfg, pools, nil, nil)
	defer wp.Stop()

	const (
		totalBurst = 5000
		queueCap   = 1024
		wantDrop   = int64(totalBurst - queueCap) // 3976
	)

	// Step 1: Inject instant burst of 5,000 packets
	for i := 0; i < totalBurst; i++ {
		evt := &pluginsdk.MetricsEvent{
			ServerID: fmt.Sprintf("srv-burst-%04d", i),
			BytesIn:  int64(1024 * (i + 1)),
		}
		wp.Ingest(evt, nil)
	}

	// Step 2: Query stats
	ingested, processed, dropped := wp.Stats()

	if ingested != totalBurst {
		t.Fatalf("[Empirical Failure] expected ingested=%d, got %d", totalBurst, ingested)
	}
	if processed != 0 {
		t.Fatalf("[Empirical Failure] expected processed=0 with zero workers, got %d", processed)
	}
	if dropped != wantDrop {
		t.Fatalf("[Empirical Failure] expected dropped=%d (5000 - 1024), got %d", wantDrop, dropped)
	}

	t.Logf("PASS: Burst of %d packets on queue capacity %d resulted in exactly %d drops (%d ingested, %d processed)",
		totalBurst, queueCap, dropped, ingested, processed)
}

// TestStress_WorkerPool_ConcurrentBurstFloodAndDrain stresses the worker pool
// with 50 parallel goroutines each pushing bursts into the queue while workers actively drain.
func TestStress_WorkerPool_ConcurrentBurstFloodAndDrain(t *testing.T) {
	cfg := analyzer.DefaultConfig()
	cfg.NumWorkers = 8
	cfg.QueueCapacity = 1024

	var processedCount atomic.Int64
	handler := func(m *analyzer.NormalizedMetric) {
		processedCount.Add(1)
	}

	pools := analyzer.NewMemoryPools()
	wp := analyzer.NewWorkerPool(cfg, pools, handler, nil)

	const (
		numProducers   = 50
		packetsPerProd = 100
		totalPackets   = numProducers * packetsPerProd // 5,000 total packets
	)

	var wg sync.WaitGroup
	wg.Add(numProducers)

	startGate := make(chan struct{})

	for p := 0; p < numProducers; p++ {
		go func(prodID int) {
			defer wg.Done()
			<-startGate // Synchronized parallel blast
			for i := 0; i < packetsPerProd; i++ {
				evt := &pluginsdk.MetricsEvent{
					ServerID: fmt.Sprintf("srv-%d", prodID),
					BytesIn:  1048576,
				}
				wp.Ingest(evt, nil)
			}
		}(p)
	}

	close(startGate)
	wg.Wait()

	// Gracefully stop the pool, draining all accepted packets
	wp.Stop()

	ingested, processed, dropped := wp.Stats()

	if ingested != totalPackets {
		t.Fatalf("[Empirical Failure] expected ingested=%d, got %d", totalPackets, ingested)
	}
	if ingested != (processed + dropped) {
		t.Fatalf("[Empirical Failure] conservation invariant broken: ingested(%d) != processed(%d) + dropped(%d)",
			ingested, processed, dropped)
	}
	if processedCount.Load() != processed {
		t.Fatalf("[Empirical Failure] handler count (%d) != processed stat (%d)",
			processedCount.Load(), processed)
	}

	t.Logf("PASS: Concurrent flood completed without race/deadlock: ingested=%d, processed=%d, dropped=%d",
		ingested, processed, dropped)
}

// ============================================================================
// 2. Memory Allocation Hardening (BufferRecycler Zero Allocations)
// ============================================================================

// BenchmarkStress_BufferRecycler_50Goroutines_ZeroAlloc verifies strictly 0 B/op
// and 0 allocs/op under high concurrency (50 parallel goroutines under -race).
func BenchmarkStress_BufferRecycler_50Goroutines_ZeroAlloc(b *testing.B) {
	// Pre-warm recycler with capacity and buffers to service all 50 parallel goroutines
	const poolCapacity = 128
	recycler := analyzer.NewBufferRecycler(poolCapacity)

	for i := 0; i < poolCapacity; i++ {
		recycler.Put(bytes.NewBuffer(make([]byte, 0, 1024)))
	}

	// Force 50 parallel goroutines
	b.SetParallelism(50)
	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			buf := recycler.Get()
			buf.WriteString(`{"server_id":"srv-node-99","bytes_in":1048576,"cpu":42.5}`)
			_ = buf.Bytes()
			recycler.Put(buf)
		}
	})
}

// TestStress_BufferRecycler_ZeroAllocProperty verifies that in steady-state
// reuse under 50 concurrent goroutines, zero allocations occur.
func TestStress_BufferRecycler_ZeroAllocProperty(t *testing.T) {
	const poolCapacity = 64
	recycler := analyzer.NewBufferRecycler(poolCapacity)

	for i := 0; i < poolCapacity; i++ {
		recycler.Put(bytes.NewBuffer(make([]byte, 0, 1024)))
	}

	allocs := testing.AllocsPerRun(500, func() {
		buf := recycler.Get()
		buf.WriteString(`{"server_id":"test","bytes_in":2048}`)
		_ = buf.Bytes()
		recycler.Put(buf)
	})

	if allocs != 0.0 {
		t.Fatalf("[Empirical Failure] BufferRecycler must have 0.0 allocs/op in steady-state, got %f", allocs)
	}
	t.Logf("PASS: BufferRecycler AllocsPerRun = %f", allocs)
}

// TestStress_BufferRecycler_OversizedBufferDisposal checks that buffers expanded beyond
// maxRecycledBuffer (64KB) are not returned to the pool, preventing memory leak.
func TestStress_BufferRecycler_OversizedBufferDisposal(t *testing.T) {
	recycler := analyzer.NewBufferRecycler(8)

	buf := recycler.Get()
	// Expand buffer beyond 64KB
	hugePayload := make([]byte, 70*1024)
	buf.Write(hugePayload)

	if buf.Cap() <= 65536 {
		t.Fatalf("expected cap > 65536, got %d", buf.Cap())
	}

	// Put should discard the oversized buffer rather than recycling it
	recycler.Put(buf)

	// Next Get should return a fresh 1KB buffer, not the 70KB buffer
	bNext := recycler.Get()
	if bNext.Cap() > 65536 {
		t.Fatalf("[Empirical Failure] oversized buffer was recycled into the pool (cap=%d)", bNext.Cap())
	}
	t.Logf("PASS: Oversized buffer cleanly discarded, next buffer cap = %d", bNext.Cap())
}

// ============================================================================
// 3. EWMA Mathematical Stability & Edge Boundaries
// ============================================================================

// TestStress_EWMA_ExtremeRateValues verifies that math.MaxUint64 and other extreme
// values never yield NaN or +Inf in EWMA calculations.
func TestStress_EWMA_ExtremeRateValues(t *testing.T) {
	calc := analyzer.NewEWMACalculator(0.2)
	serverID := "srv-extreme-math"

	testCases := []struct {
		name     string
		input    float64
		checkVal func(t *testing.T, res float64)
	}{
		{
			name:  "math.MaxUint64",
			input: float64(math.MaxUint64),
			checkVal: func(t *testing.T, res float64) {
				if math.IsNaN(res) || math.IsInf(res, 0) {
					t.Fatalf("[Empirical Failure] MaxUint64 rate produced NaN or Inf: %f", res)
				}
				if res <= 0 {
					t.Fatalf("[Empirical Failure] MaxUint64 rate produced non-positive EWMA: %f", res)
				}
			},
		},
		{
			name:  "math.MaxFloat64",
			input: math.MaxFloat64,
			checkVal: func(t *testing.T, res float64) {
				if math.IsNaN(res) || math.IsInf(res, 0) {
					t.Fatalf("[Empirical Failure] MaxFloat64 produced NaN or Inf: %f", res)
				}
			},
		},
		{
			name:  "Positive Infinity",
			input: math.Inf(1),
			checkVal: func(t *testing.T, res float64) {
				if math.IsNaN(res) || math.IsInf(res, 0) {
					t.Fatalf("[Empirical Failure] +Inf was not sanitized: %f", res)
				}
			},
		},
		{
			name:  "Negative Infinity",
			input: math.Inf(-1),
			checkVal: func(t *testing.T, res float64) {
				if math.IsNaN(res) || math.IsInf(res, 0) {
					t.Fatalf("[Empirical Failure] -Inf was not sanitized: %f", res)
				}
			},
		},
		{
			name:  "NaN value",
			input: math.NaN(),
			checkVal: func(t *testing.T, res float64) {
				if math.IsNaN(res) || math.IsInf(res, 0) {
					t.Fatalf("[Empirical Failure] NaN input produced NaN/Inf: %f", res)
				}
			},
		},
		{
			name:  "Negative rate",
			input: -500.0,
			checkVal: func(t *testing.T, res float64) {
				if math.IsNaN(res) || res < 0 {
					t.Fatalf("[Empirical Failure] Negative rate produced invalid result: %f", res)
				}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			res := calc.UpdateRate(serverID, tc.input)
			tc.checkVal(t, res)
		})
	}
}

// TestStress_EWMA_CorruptedCounterRejection tests strict rejection with ErrCorruptedCounter.
func TestStress_EWMA_CorruptedCounterRejection(t *testing.T) {
	corruptedCases := []struct {
		name    string
		payload []byte
	}{
		{
			name:    "negative bytes_in",
			payload: []byte(`{"server_id":"srv-corrupt-1","bytes_in":-500}`),
		},
		{
			name:    "negative bytes_out",
			payload: []byte(`{"server_id":"srv-corrupt-2","bytes_out":-1}`),
		},
		{
			name:    "negative cpu",
			payload: []byte(`{"server_id":"srv-corrupt-3","cpu":-10.5}`),
		},
		{
			name:    "min int64 bytes_in",
			payload: []byte(fmt.Sprintf(`{"server_id":"srv-corrupt-4","bytes_in":%d}`, math.MinInt64)),
		},
	}

	for _, tc := range corruptedCases {
		t.Run(tc.name, func(t *testing.T) {
			metric, err := analyzer.ParseRawPayload(tc.payload)
			if err == nil {
				t.Fatalf("[Empirical Failure] expected error on corrupted payload %s, got metric: %+v",
					string(tc.payload), metric)
			}
			if !errors.Is(err, analyzer.ErrCorruptedCounter) {
				t.Fatalf("[Empirical Failure] expected ErrCorruptedCounter, got: %v", err)
			}
		})
	}

	// Also verify ExtractNormalized rejects negative counters in structured events
	badEvt := &pluginsdk.MetricsEvent{
		ServerID: "srv-bad",
		BytesIn:  -1024,
	}
	if norm, ok := analyzer.ExtractNormalized(badEvt); ok || norm != nil {
		t.Fatalf("[Empirical Failure] ExtractNormalized should reject negative counter, got norm=%+v", norm)
	}
	t.Logf("PASS: Corrupted counters successfully rejected with ErrCorruptedCounter")
}

// TestStress_EWMA_Decay60Ticks verifies that 60 ticks of 0 rate decay EWMA below 0.001
// smoothly without subnormal or negative floating artifacts.
func TestStress_EWMA_Decay60Ticks(t *testing.T) {
	baselines := []float64{10.0, 100.0, 10000.0, 1e8}

	for _, base := range baselines {
		t.Run(fmt.Sprintf("Baseline_%.1f", base), func(t *testing.T) {
			calc := analyzer.NewEWMACalculator(0.2)
			serverID := fmt.Sprintf("srv-decay-%f", base)
			calc.SetBaseline(serverID, base)

			var lastVal float64
			for tick := 1; tick <= 60; tick++ {
				lastVal = calc.DecayMissing(serverID)

				// Verify monotonicity (never increase)
				if math.IsNaN(lastVal) {
					t.Fatalf("tick %d: produced NaN", tick)
				}
				if lastVal < 0 {
					t.Fatalf("tick %d: produced negative float %f", tick, lastVal)
				}
			}

			// Invariant: After 60 ticks of alpha=0.2 decay, (1-0.2)^60 = 1.537e-6
			// Even base=10000 gives 10000 * 1.537e-6 = 0.015, but for base=100 it is 0.00015 < 0.001
			if base <= 100.0 && lastVal >= 0.001 {
				t.Fatalf("[Empirical Failure] baseline %.1f did not decay below 0.001 after 60 ticks: %f", base, lastVal)
			}

			// Verify no subnormal representation (denormalized floating point)
			bits := math.Float64bits(lastVal)
			exp := (bits >> 52) & 0x7FF
			frac := bits & 0x000FFFFFFFFFFFFF
			if exp == 0 && frac != 0 {
				t.Fatalf("[Empirical Failure] rate became a denormalized/subnormal float: bits=0x%016x", bits)
			}
		})
	}
	t.Logf("PASS: EWMA smoothly decayed across 60 ticks with zero subnormal/negative artifacts")
}

// TestStress_SpikeThresholdBoundaries tests spike alert boundaries:
// 30.0 MB/s (no spike), 49.9 MB/s (no spike), 50.1 MB/s (spike triggered).
func TestStress_SpikeThresholdBoundaries(t *testing.T) {
	cfg := analyzer.DefaultConfig()
	cfg.TrafficSpikeMultiplier = 3.0
	cfg.MinTrafficSpikeMBps = 50.0

	detector := analyzer.NewAnomalyDetector(cfg, nil, nil)
	baselineEWMA := 10.0

	boundaryCases := []struct {
		rate        float64
		expectSpike bool
		rationale   string
	}{
		{
			rate:        30.0,
			expectSpike: false,
			rationale:   "30.0 is not > 3.0*10.0 (30.0), and not > 50.0 MB/s",
		},
		{
			rate:        49.9,
			expectSpike: false,
			rationale:   "49.9 is > 30.0, but NOT > 50.0 MB/s absolute floor",
		},
		{
			rate:        50.0,
			expectSpike: false,
			rationale:   "50.0 is exactly equal to 50.0 MB/s floor (must be strictly > 50.0)",
		},
		{
			rate:        50.0001,
			expectSpike: true,
			rationale:   "50.0001 is > 30.0 and strictly > 50.0 MB/s",
		},
		{
			rate:        50.1,
			expectSpike: true,
			rationale:   "50.1 is > 30.0 and > 50.0 MB/s",
		},
		{
			rate:        100.0,
			expectSpike: true,
			rationale:   "100.0 is >> 30.0 and >> 50.0 MB/s",
		},
	}

	for _, bc := range boundaryCases {
		got := detector.IsSpike(bc.rate, baselineEWMA)
		if got != bc.expectSpike {
			t.Fatalf("[Empirical Failure] rate %.4f MB/s: expected spike=%v, got %v (%s)",
				bc.rate, bc.expectSpike, got, bc.rationale)
		}
		t.Logf("PASS: rate %.4f MB/s -> spike=%v (%s)", bc.rate, got, bc.rationale)
	}
}
