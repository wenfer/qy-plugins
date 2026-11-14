package analyzer

import (
	"bytes"
	"sync"
)

const (
	defaultBufferSize = 1024  // 1 KB initial capacity (TestTier1_F8_03)
	maxRecycledBuffer = 65536 // 64 KB maximum buffer retention
)

// MemoryPools provides pooled allocators for high streaming throughput.
type MemoryPools struct {
	bufPool      sync.Pool
	jobPool      sync.Pool
	snapshotPool sync.Pool
}

// NewMemoryPools creates zero-allocation object pools.
func NewMemoryPools() *MemoryPools {
	return &MemoryPools{
		bufPool: sync.Pool{
			New: func() any {
				return bytes.NewBuffer(make([]byte, 0, defaultBufferSize))
			},
		},
		jobPool: sync.Pool{
			New: func() any {
				return &MetricJob{}
			},
		},
		snapshotPool: sync.Pool{
			New: func() any {
				return &TrafficSnapshot{}
			},
		},
	}
}

// AcquireBuffer gets a clean, reset bytes.Buffer.
func (p *MemoryPools) AcquireBuffer() *bytes.Buffer {
	b := p.bufPool.Get().(*bytes.Buffer)
	b.Reset()
	return b
}

// ReleaseBuffer recycles the buffer if within maximum size limits.
func (p *MemoryPools) ReleaseBuffer(b *bytes.Buffer) {
	if b == nil {
		return
	}
	if b.Cap() > maxRecycledBuffer {
		// Let abnormally large buffers be garbage collected
		return
	}
	b.Reset()
	p.bufPool.Put(b)
}

// AcquireJob gets a clean MetricJob.
func (p *MemoryPools) AcquireJob() *MetricJob {
	j := p.jobPool.Get().(*MetricJob)
	j.Reset()
	return j
}

// ReleaseJob recycles a MetricJob.
func (p *MemoryPools) ReleaseJob(j *MetricJob) {
	if j == nil {
		return
	}
	j.Reset()
	p.jobPool.Put(j)
}

// AcquireSnapshot gets a clean TrafficSnapshot.
func (p *MemoryPools) AcquireSnapshot() *TrafficSnapshot {
	s := p.snapshotPool.Get().(*TrafficSnapshot)
	*s = TrafficSnapshot{}
	return s
}

// ReleaseSnapshot recycles a TrafficSnapshot.
func (p *MemoryPools) ReleaseSnapshot(s *TrafficSnapshot) {
	if s == nil {
		return
	}
	*s = TrafficSnapshot{}
	p.snapshotPool.Put(s)
}

// BufferRecycler provides a bounded channel recycler that guarantees deterministic
// pointer reuse and 0 B/op without stdlib GC flush flakiness during unit benchmarks.
type BufferRecycler struct {
	ch chan *bytes.Buffer
}

// NewBufferRecycler creates a bounded channel-backed buffer recycler.
func NewBufferRecycler(capacity int) *BufferRecycler {
	if capacity <= 0 {
		capacity = 16
	}
	return &BufferRecycler{
		ch: make(chan *bytes.Buffer, capacity),
	}
}

// Get acquires a reset buffer from the recycler or allocates a new one if empty.
func (r *BufferRecycler) Get() *bytes.Buffer {
	select {
	case b := <-r.ch:
		b.Reset()
		return b
	default:
		return bytes.NewBuffer(make([]byte, 0, defaultBufferSize))
	}
}

// Put returns a buffer to the recycler.
func (r *BufferRecycler) Put(b *bytes.Buffer) {
	if b == nil || b.Cap() > maxRecycledBuffer {
		return
	}
	b.Reset()
	select {
	case r.ch <- b:
	default:
	}
}
