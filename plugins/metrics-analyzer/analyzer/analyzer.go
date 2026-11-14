package analyzer

import (
	"context"
	"fmt"
	"sync"

	"github.com/wenfer/qy-plugins/pkg/pluginsdk"
)

// MetricsAnalyzerPlugin implements the pluginsdk.Plugin interface.
type MetricsAnalyzerPlugin struct {
	cfg        Config
	host       pluginsdk.HostContext
	pools      *MemoryPools
	calc       *EWMACalculator
	workerPool *WorkerPool
	detector   *AnomalyDetector
	flusher    *StorageFlusher
	sub        pluginsdk.MetricsSubscription
	logger     pluginsdk.Logger

	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
}

// New creates a new MetricsAnalyzerPlugin instance.
func New(cfg Config) *MetricsAnalyzerPlugin {
	return &MetricsAnalyzerPlugin{
		cfg:   cfg,
		pools: NewMemoryPools(),
		calc:  NewEWMACalculator(cfg.EWMAAlpha),
	}
}

// Calculator returns the internal EWMA calculator.
func (p *MetricsAnalyzerPlugin) Calculator() *EWMACalculator {
	return p.calc
}

// Pools returns the memory recyclers.
func (p *MetricsAnalyzerPlugin) Pools() *MemoryPools {
	return p.pools
}

// WorkerPool returns the active worker pool.
func (p *MetricsAnalyzerPlugin) WorkerPool() *WorkerPool {
	return p.workerPool
}

// Start is called by pluginsdk.Serve once the UDS handshake succeeds.
func (p *MetricsAnalyzerPlugin) Start(host pluginsdk.HostContext) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.host = host
	p.logger = host.Log()
	p.ctx, p.cancel = context.WithCancel(host.Context())

	p.logger.Infof("Initializing Metrics Analyzer Plugin (Workers=%d, Queue=%d, Alpha=%.2f)",
		p.cfg.NumWorkers, p.cfg.QueueCapacity, p.cfg.EWMAAlpha)

	// Initialize anomaly detector & storage flusher
	p.detector = NewAnomalyDetector(p.cfg, host, p.logger)
	p.flusher = NewStorageFlusher(p.cfg, host, p.calc, p.pools, p.logger)

	// Telemetry event handler passed to workers
	handler := func(metric *NormalizedMetric) {
		state := p.calc.Update(metric)
		p.detector.Evaluate(p.ctx, state)
	}

	// Initialize worker pool
	p.workerPool = NewWorkerPool(p.cfg, p.pools, handler, p.logger)

	// Subscribe to host probe metrics stream
	subOpts := pluginsdk.SubscribeOptions{
		ServerIDs:  nil, // Subscribe to all servers
		BufferSize: p.cfg.QueueCapacity,
	}

	sub, err := host.SubscribeMetrics(p.ctx, subOpts, pluginsdk.MetricsHandlerFunc(func(event *pluginsdk.MetricsEvent) {
		p.workerPool.Ingest(event, nil)
	}))
	if err != nil {
		return fmt.Errorf("subscribe metrics: %w", err)
	}
	p.sub = sub

	p.logger.Infof("Metrics Analyzer Plugin successfully started and subscribed to telemetry stream")
	return nil
}

// Stop is called during graceful shutdown (SIGTERM or host ShutdownReq).
func (p *MetricsAnalyzerPlugin) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.logger != nil {
		p.logger.Infof("Stopping Metrics Analyzer Plugin...")
	}

	if p.cancel != nil {
		p.cancel()
	}

	if p.sub != nil {
		_ = p.sub.Close()
	}

	if p.workerPool != nil {
		p.workerPool.Stop()
	}

	if p.flusher != nil {
		p.flusher.Stop()
	}

	if p.logger != nil {
		p.logger.Infof("Metrics Analyzer Plugin stopped cleanly")
	}

	return nil
}
