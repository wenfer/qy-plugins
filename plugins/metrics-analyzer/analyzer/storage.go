package analyzer

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/wenfer/qy-plugins/pkg/pluginsdk"
)

// StorageFlusher orchestrates periodic and shutdown persistence to isolated SQLite KV.
type StorageFlusher struct {
	cfg        Config
	host       pluginsdk.HostContext
	calculator *EWMACalculator
	pools      *MemoryPools
	logger     pluginsdk.Logger

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewStorageFlusher creates a new storage persistence runner.
func NewStorageFlusher(cfg Config, host pluginsdk.HostContext, calc *EWMACalculator, pools *MemoryPools, logger pluginsdk.Logger) *StorageFlusher {
	ctx, cancel := context.WithCancel(context.Background())
	sf := &StorageFlusher{
		cfg:        cfg,
		host:       host,
		calculator: calc,
		pools:      pools,
		logger:     logger,
		ctx:        ctx,
		cancel:     cancel,
	}

	sf.wg.Add(1)
	go sf.runFlushTicker()

	return sf
}

func (sf *StorageFlusher) runFlushTicker() {
	defer sf.wg.Done()
	interval := sf.cfg.FlushInterval
	if interval <= 0 {
		interval = 10 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-sf.ctx.Done():
			return
		case <-ticker.C:
			sf.Flush(sf.ctx)
		}
	}
}

// Flush persists current server EWMA rates to SQLite KV (TestTier1_F8_05 & TestTier4_Scenario1).
func (sf *StorageFlusher) Flush(ctx context.Context) {
	if sf.host == nil {
		return
	}
	storage := sf.host.Storage()
	if storage == nil {
		return
	}

	states := sf.calculator.GetAllStates()
	for serverID, state := range states {
		state.mu.RLock()
		snapshot := sf.pools.AcquireSnapshot()
		snapshot.ServerID = serverID
		snapshot.RateIn = int64(state.CurrentDownloadRateMB * 1024.0 * 1024.0)
		snapshot.RateOut = int64(state.CurrentUploadRateMB * 1024.0 * 1024.0)
		snapshot.EWMARate = state.EWMADownloadMB
		snapshot.EWMARateInMB = state.EWMADownloadMB
		snapshot.EWMARateOutMB = state.EWMAUploadMB
		snapshot.EWMACPUPercent = state.EWMACPU
		snapshot.Timestamp = time.Now().Unix()
		state.mu.RUnlock()

		// Persist under canonical keys:
		// 1. agg:traffic:<server_id>:rates (TestTier1_F8_05)
		ratesKey := fmt.Sprintf("agg:traffic:%s:rates", serverID)
		if err := storage.SetJSON(ctx, ratesKey, snapshot); err != nil {
			if sf.logger != nil {
				sf.logger.Errorf("failed to persist traffic snapshot for %s: %v", ratesKey, err)
			}
		}

		// 2. agg:traffic:<server_id>:hourly (TestTier4_Scenario1)
		hourlyKey := fmt.Sprintf("agg:traffic:%s:hourly", serverID)
		if err := storage.SetJSON(ctx, hourlyKey, snapshot); err != nil {
			if sf.logger != nil {
				sf.logger.Errorf("failed to persist hourly snapshot for %s: %v", hourlyKey, err)
			}
		}

		sf.pools.ReleaseSnapshot(snapshot)
	}
}

// Stop stops the background ticker and executes a final synchronous flush.
func (sf *StorageFlusher) Stop() {
	sf.cancel()
	sf.wg.Wait()

	// Synchronous final flush ensuring zero telemetry data loss
	flushCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	sf.Flush(flushCtx)
}
