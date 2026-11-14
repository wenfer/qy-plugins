package ipc

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
)

// 1. Stress test corrupted headers and fuzzing
func TestStressCorruptedHeadersAndFuzzing(t *testing.T) {
	// Test various magic numbers that are NOT Magic (0x5159504C)
	corruptMagics := []uint32{
		0x00000000,
		0xFFFFFFFF,
		0x5159504D, // 1 bit flip
		0x51595040,
		0x12345678,
		0x504C5951, // LittleEndian byte order of Magic
	}

	for _, m := range corruptMagics {
		var hdr [HeaderSize]byte
		binary.BigEndian.PutUint32(hdr[0:4], m)
		hdr[4] = Version
		_, err := ReadFrame(bytes.NewReader(hdr[:]))
		if !errors.Is(err, ErrInvalidMagic) {
			t.Errorf("magic 0x%08X: expected ErrInvalidMagic, got %v", m, err)
		}
	}

	// Test invalid versions
	invalidVersions := []uint8{0x00, 0x02, 0x05, 0xFF}
	for _, v := range invalidVersions {
		var hdr [HeaderSize]byte
		binary.BigEndian.PutUint32(hdr[0:4], Magic)
		hdr[4] = v
		_, err := ReadFrame(bytes.NewReader(hdr[:]))
		if !errors.Is(err, ErrUnsupportedVersion) {
			t.Errorf("version 0x%02X: expected ErrUnsupportedVersion, got %v", v, err)
		}
	}

	// Fuzz test: Send 5,000 random 16-byte headers
	for i := 0; i < 5000; i++ {
		var fuzzHdr [HeaderSize]byte
		_, _ = rand.Read(fuzzHdr[:])

		magic := binary.BigEndian.Uint32(fuzzHdr[0:4])
		version := fuzzHdr[4]
		length := binary.BigEndian.Uint32(fuzzHdr[12:16])

		_, err := ReadFrame(bytes.NewReader(fuzzHdr[:]))
		if magic != Magic {
			if !errors.Is(err, ErrInvalidMagic) {
				t.Fatalf("fuzz iteration %d: expected ErrInvalidMagic for magic 0x%08X, got %v", i, magic, err)
			}
		} else if version != Version {
			if !errors.Is(err, ErrUnsupportedVersion) {
				t.Fatalf("fuzz iteration %d: expected ErrUnsupportedVersion for ver 0x%02X, got %v", i, version, err)
			}
		} else if length > MaxPayloadSize {
			if !errors.Is(err, ErrPayloadTooLarge) {
				t.Fatalf("fuzz iteration %d: expected ErrPayloadTooLarge for len %d, got %v", i, length, err)
			}
		}
	}
}

// 2. Stress test boundary limits (>16MB, max uint32)
func TestStressBoundaryLimits(t *testing.T) {
	oversizedLengths := []uint32{
		MaxPayloadSize + 1,
		MaxPayloadSize + 1024,
		32 * 1024 * 1024,
		0x7FFFFFFF, // max int32
		0x80000000,
		0xFFFFFFFF, // max uint32
	}

	for _, length := range oversizedLengths {
		var hdr [HeaderSize]byte
		binary.BigEndian.PutUint32(hdr[0:4], Magic)
		hdr[4] = Version
		hdr[5] = byte(TypePing)
		binary.BigEndian.PutUint32(hdr[12:16], length)

		// ReadFrame should reject immediately based on header without attempting to read payload
		_, err := ReadFrame(bytes.NewReader(hdr[:]))
		if !errors.Is(err, ErrPayloadTooLarge) {
			t.Errorf("length %d: expected ErrPayloadTooLarge, got %v", length, err)
		}

		// ReadFramePooled should also reject immediately
		_, err = ReadFramePooled(bytes.NewReader(hdr[:]))
		if !errors.Is(err, ErrPayloadTooLarge) {
			t.Errorf("ReadFramePooled length %d: expected ErrPayloadTooLarge, got %v", length, err)
		}

		// DecodeFrame should reject immediately
		data := make([]byte, HeaderSize+10)
		copy(data, hdr[:])
		_, err = DecodeFrame(data)
		if !errors.Is(err, ErrPayloadTooLarge) {
			t.Errorf("DecodeFrame length %d: expected ErrPayloadTooLarge, got %v", length, err)
		}
	}
}

// 3. Stress test fragmented streaming (1-byte reader simulating slow or fragmented socket)
type oneByteReader struct {
	r io.Reader
}

func (obr *oneByteReader) Read(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}
	return obr.r.Read(p[:1])
}

func TestFragmentedStreamReads(t *testing.T) {
	payload := []byte("Testing fragmented 1-byte delivery over Unix domain socket connection!")
	frame := NewFrame(TypeStorageSetReq, 12345, payload)
	encoded, err := EncodeFrame(frame)
	if err != nil {
		t.Fatalf("EncodeFrame failed: %v", err)
	}

	// Feed through 1-byte reader
	obr := &oneByteReader{r: bytes.NewReader(encoded)}
	decoded, err := ReadFrame(obr)
	if err != nil {
		t.Fatalf("ReadFrame with 1-byte chunks failed: %v", err)
	}

	if decoded.SeqID != 12345 || !bytes.Equal(decoded.Payload, payload) {
		t.Fatalf("Frame content corrupted during fragmented read")
	}

	// Also test pooled read through 1-byte reader
	obr2 := &oneByteReader{r: bytes.NewReader(encoded)}
	decodedPooled, err := ReadFramePooled(obr2)
	if err != nil {
		t.Fatalf("ReadFramePooled with 1-byte chunks failed: %v", err)
	}
	defer ReleaseFrame(decodedPooled)

	if decodedPooled.SeqID != 12345 || !bytes.Equal(decodedPooled.Payload, payload) {
		t.Fatalf("Pooled frame content corrupted during fragmented read")
	}
}

// 4. Stress test sync.Pool allocation stability and concurrency under heavy throughput
func TestSyncPoolAllocationStabilityUnderLoad(t *testing.T) {
	const (
		concurrency = 20
		itersPerG   = 2000
	)

	// Pre-encode frames of various tiered sizes
	sizes := []int{
		128,              // Small tier (<= 4KB)
		SmallBufferSize,  // 4KB exactly
		16 * 1024,        // Medium tier (<= 64KB)
		MediumBufferSize, // 64KB exactly
		256 * 1024,       // Large tier (<= 512KB)
		LargeBufferSize,  // 512KB exactly
		600 * 1024,       // > 512KB fallback
	}

	var encodedFrames [][]byte
	for i, size := range sizes {
		buf := make([]byte, size)
		for j := range buf {
			buf[j] = byte(i + j)
		}
		f := NewFrame(TypeMetricsEvent, uint32(i+1), buf)
		enc, err := EncodeFrame(f)
		if err != nil {
			t.Fatalf("failed to encode frame of size %d: %v", size, err)
		}
		encodedFrames = append(encodedFrames, enc)
	}

	var wg sync.WaitGroup
	var totalProcessed int64

	wg.Add(concurrency)
	for g := 0; g < concurrency; g++ {
		go func(gid int) {
			defer wg.Done()
			for iter := 0; iter < itersPerG; iter++ {
				idx := (gid + iter) % len(encodedFrames)
				raw := encodedFrames[idx]

				r := bytes.NewReader(raw)
				f, err := ReadFramePooled(r)
				if err != nil {
					t.Errorf("gid %d iter %d ReadFramePooled failed: %v", gid, iter, err)
					return
				}

				// Verify payload integrity
				if int(f.Length) != sizes[idx] {
					t.Errorf("length mismatch: expected %d, got %d", sizes[idx], f.Length)
					ReleaseFrame(f)
					return
				}
				if len(f.Payload) != sizes[idx] {
					t.Errorf("payload slice mismatch: expected %d, got %d", sizes[idx], len(f.Payload))
					ReleaseFrame(f)
					return
				}
				if f.Payload[0] != byte(idx) {
					t.Errorf("payload byte mismatch: expected %d, got %d", byte(idx), f.Payload[0])
					ReleaseFrame(f)
					return
				}

				ReleaseFrame(f)
				atomic.AddInt64(&totalProcessed, 1)
			}
		}(g)
	}

	wg.Wait()

	expectedTotal := int64(concurrency * itersPerG)
	if totalProcessed != expectedTotal {
		t.Errorf("expected %d processed frames, got %d", expectedTotal, totalProcessed)
	}
}

// 5. Stress test net.Pipe concurrent bi-directional streaming
func TestBidirectionalConcurrentStreaming(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	const msgCount = 500
	var wg sync.WaitGroup
	wg.Add(2)

	// Peer 1: Writes Ping, Reads Pong
	go func() {
		defer wg.Done()
		for i := uint32(1); i <= msgCount; i++ {
			f := NewFrame(TypePing, i, []byte("ping-data"))
			if err := WriteFrame(c1, f); err != nil {
				t.Errorf("Peer 1 WriteFrame failed at %d: %v", i, err)
				return
			}
			resp, err := ReadFrame(c1)
			if err != nil {
				t.Errorf("Peer 1 ReadFrame failed at %d: %v", i, err)
				return
			}
			if resp.Type != TypePong || resp.SeqID != i {
				t.Errorf("Peer 1 unexpected resp: type=%v, seq=%d", resp.Type, resp.SeqID)
				return
			}
		}
	}()

	// Peer 2: Reads Ping, Writes Pong
	go func() {
		defer wg.Done()
		for i := uint32(1); i <= msgCount; i++ {
			req, err := ReadFrame(c2)
			if err != nil {
				t.Errorf("Peer 2 ReadFrame failed at %d: %v", i, err)
				return
			}
			if req.Type != TypePing || req.SeqID != i {
				t.Errorf("Peer 2 unexpected req: type=%v, seq=%d", req.Type, req.SeqID)
				return
			}
			resp := NewFrame(TypePong, req.SeqID, req.Payload)
			if err := WriteFrame(c2, resp); err != nil {
				t.Errorf("Peer 2 WriteFrame failed at %d: %v", i, err)
				return
			}
		}
	}()

	wg.Wait()
}
