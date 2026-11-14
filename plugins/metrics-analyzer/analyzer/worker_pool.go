package analyzer

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wenfer/qy-plugins/pkg/pluginsdk"
)

// WorkerPool manages multi-threaded telemetry processing with a bounded job queue.
type WorkerPool struct {
	cfg      Config
	pools    *MemoryPools
	jobQueue chan *MetricJob
	handler  func(metric *NormalizedMetric)
	logger   pluginsdk.Logger

	metricsIngested  atomic.Int64
	metricsProcessed atomic.Int64
	metricsDropped   atomic.Int64 // Saturation drop counter for TestTier2_F8_01

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewWorkerPool creates and starts the worker pool.
func NewWorkerPool(cfg Config, pools *MemoryPools, handler func(*NormalizedMetric), logger pluginsdk.Logger) *WorkerPool {
	ctx, cancel := context.WithCancel(context.Background())
	wp := &WorkerPool{
		cfg:      cfg,
		pools:    pools,
		jobQueue: make(chan *MetricJob, cfg.QueueCapacity),
		handler:  handler,
		logger:   logger,
		ctx:      ctx,
		cancel:   cancel,
	}

	for i := 0; i < cfg.NumWorkers; i++ {
		wp.wg.Add(1)
		go wp.workerLoop(i)
	}

	return wp
}

// Ingest attempts to enqueue an incoming telemetry event.
// Under channel saturation, it immediately drops the packet and increments metricsDropped (TestTier2_F8_01).
func (wp *WorkerPool) Ingest(event *pluginsdk.MetricsEvent, raw []byte) bool {
	wp.metricsIngested.Add(1)

	job := wp.pools.AcquireJob()
	job.Event = event
	job.ReceivedAt = time.Now()

	if len(raw) > 0 {
		buf := wp.pools.AcquireBuffer()
		buf.Write(raw)
		job.RawData = buf.Bytes()
	}

	select {
	case wp.jobQueue <- job:
		return true
	default:
		// Queue full: drop packet without blocking or crashing (TestTier2_F8_01)
		wp.metricsDropped.Add(1)
		wp.pools.ReleaseJob(job)
		return false
	}
}

// Submit enqueues an explicit MetricJob (used in direct worker pool throughput tests).
func (wp *WorkerPool) Submit(job *MetricJob) bool {
	wp.metricsIngested.Add(1)
	select {
	case wp.jobQueue <- job:
		return true
	default:
		wp.metricsDropped.Add(1)
		if job.OnComplete != nil {
			job.OnComplete()
		}
		return false
	}
}

func (wp *WorkerPool) workerLoop(id int) {
	defer wp.wg.Done()

	for {
		select {
		case <-wp.ctx.Done():
			// Drain remaining items from queue during stop
			for {
				select {
				case job := <-wp.jobQueue:
					if job != nil {
						wp.processJob(job)
						wp.pools.ReleaseJob(job)
					}
				default:
					return
				}
			}
		case job, ok := <-wp.jobQueue:
			if !ok {
				return
			}
			wp.processJob(job)
			wp.pools.ReleaseJob(job)
		}
	}
}

func (wp *WorkerPool) processJob(job *MetricJob) {
	defer func() {
		if job.OnComplete != nil {
			job.OnComplete()
		}
	}()

	var metric *NormalizedMetric
	var ok bool

	if job.Event != nil {
		metric, ok = ExtractNormalized(job.Event)
	}
	if !ok && len(job.RawData) > 0 {
		var err error
		metric, err = ParseRawPayload(job.RawData)
		if err != nil {
			if wp.logger != nil {
				wp.logger.Warnf("rejected corrupted metric payload: %v", err)
			}
			return
		}
	}
	if !ok && metric == nil && job.ServerID != "" {
		metric = &NormalizedMetric{
			ServerID:       job.ServerID,
			Timestamp:      time.Now(),
			DownloadRateMB: float64(job.BytesIn) / (1024.0 * 1024.0),
			UploadRateMB:   float64(job.BytesOut) / (1024.0 * 1024.0),
			TotalRecvBytes: uint64(job.BytesIn),
			TotalSentBytes: uint64(job.BytesOut),
		}
	}

	if metric == nil || metric.ServerID == "" {
		return
	}

	if wp.handler != nil {
		wp.handler(metric)
	}
	wp.metricsProcessed.Add(1)
}

// Stop gracefully shuts down the worker pool, draining pending jobs.
func (wp *WorkerPool) Stop() {
	wp.cancel()
	wp.wg.Wait()
}

// Stats returns runtime throughput and saturation counters.
func (wp *WorkerPool) Stats() (ingested, processed, dropped int64) {
	return wp.metricsIngested.Load(), wp.metricsProcessed.Load(), wp.metricsDropped.Load()
}
