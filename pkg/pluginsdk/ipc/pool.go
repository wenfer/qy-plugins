package ipc

import (
	"sync"
)

// Tiered buffer bucket limits
const (
	SmallBufferSize  = 4096   // 4 KB: heartbeats, small RPC requests
	MediumBufferSize = 65536  // 64 KB: single server metrics events
	LargeBufferSize  = 524288 // 512 KB: full server lists
)

var (
	headerPool = sync.Pool{
		New: func() any {
			b := make([]byte, HeaderSize)
			return &b
		},
	}

	framePool = sync.Pool{
		New: func() any {
			return &Frame{}
		},
	}

	smallPool = sync.Pool{
		New: func() any {
			return make([]byte, SmallBufferSize)
		},
	}

	mediumPool = sync.Pool{
		New: func() any {
			return make([]byte, MediumBufferSize)
		},
	}

	largePool = sync.Pool{
		New: func() any {
			return make([]byte, LargeBufferSize)
		},
	}
)

func acquireHeaderBuf() *[]byte {
	return headerPool.Get().(*[]byte)
}

func releaseHeaderBuf(b *[]byte) {
	headerPool.Put(b)
}

// AcquireFrame retrieves a clean Frame from the pool.
func AcquireFrame() *Frame {
	f := framePool.Get().(*Frame)
	f.Reset()
	return f
}

// ReleaseFrame returns a Frame and its pooled payload buffer to their respective pools.
func ReleaseFrame(f *Frame) {
	if f == nil {
		return
	}
	if cap(f.Payload) > 0 {
		PutBuffer(f.Payload)
	}
	f.Payload = nil
	framePool.Put(f)
}

// GetBuffer retrieves a byte slice with at least the requested size from tiered pools.
func GetBuffer(size int) []byte {
	if size <= SmallBufferSize {
		b := smallPool.Get().([]byte)
		return b[:size]
	}
	if size <= MediumBufferSize {
		b := mediumPool.Get().([]byte)
		return b[:size]
	}
	if size <= LargeBufferSize {
		b := largePool.Get().([]byte)
		return b[:size]
	}
	return make([]byte, size)
}

// PutBuffer recycles byte slices into the matching tiered pool if size matches.
func PutBuffer(b []byte) {
	c := cap(b)
	switch c {
	case SmallBufferSize:
		smallPool.Put(b[:SmallBufferSize])
	case MediumBufferSize:
		mediumPool.Put(b[:MediumBufferSize])
	case LargeBufferSize:
		largePool.Put(b[:LargeBufferSize])
	default:
		// Slices > 512KB or abnormal capacities are left for GC collection.
	}
}
