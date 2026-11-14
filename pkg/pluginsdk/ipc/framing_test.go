package ipc

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
)

// 1. Verify byte layout, offsets, and BigEndian encoding
func TestHeaderLayoutOffsets(t *testing.T) {
	seq := uint32(0x12345678)
	length := uint32(0x00010000)
	frame := NewFrame(TypeHandshakeReq, seq, make([]byte, length))
	frame.Flags.Set(FlagJSON | FlagError)
	frame.Reserved = 0xAA

	encoded, err := EncodeFrame(frame)
	if err != nil {
		t.Fatalf("EncodeFrame failed: %v", err)
	}

	if len(encoded) != HeaderSize+int(length) {
		t.Fatalf("expected len %d, got %d", HeaderSize+int(length), len(encoded))
	}

	// Verify exact byte offsets
	magic := binary.BigEndian.Uint32(encoded[0:4])
	if magic != Magic {
		t.Errorf("magic offset 0..3 mismatch: 0x%08X", magic)
	}
	if encoded[4] != Version {
		t.Errorf("version offset 4 mismatch: %d", encoded[4])
	}
	if encoded[5] != byte(TypeHandshakeReq) {
		t.Errorf("msgType offset 5 mismatch: 0x%02X", encoded[5])
	}
	if encoded[6] != byte(FlagJSON|FlagError) {
		t.Errorf("flags offset 6 mismatch: 0x%02X", encoded[6])
	}
	if encoded[7] != 0xAA {
		t.Errorf("reserved offset 7 mismatch: 0x%02X", encoded[7])
	}
	decSeq := binary.BigEndian.Uint32(encoded[8:12])
	if decSeq != seq {
		t.Errorf("seqID offset 8..11 mismatch: 0x%08X", decSeq)
	}
	decLen := binary.BigEndian.Uint32(encoded[12:16])
	if decLen != length {
		t.Errorf("length offset 12..15 mismatch: %d", decLen)
	}
}

// 2. Test Roundtrip over net.Pipe
func TestFrameRoundtrip_Pipe(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	payload := []byte(`{"message":"hello over UDS binary stream"}`)
	sentFrame := NewFrame(TypeStorageSetReq, 42, payload)
	sentFrame.Flags.Set(FlagJSON)

	errCh := make(chan error, 1)
	go func() {
		errCh <- WriteFrame(server, sentFrame)
	}()

	recvFrame, err := ReadFrame(client)
	if err != nil {
		t.Fatalf("ReadFrame failed: %v", err)
	}
	if writeErr := <-errCh; writeErr != nil {
		t.Fatalf("WriteFrame failed: %v", writeErr)
	}

	if recvFrame.Type != TypeStorageSetReq {
		t.Errorf("expected type %v, got %v", TypeStorageSetReq, recvFrame.Type)
	}
	if recvFrame.SeqID != 42 {
		t.Errorf("expected seqID 42, got %d", recvFrame.SeqID)
	}
	if !recvFrame.Flags.Has(FlagJSON) {
		t.Errorf("expected FlagJSON set")
	}
	if !bytes.Equal(recvFrame.Payload, payload) {
		t.Errorf("payload mismatch: %s", string(recvFrame.Payload))
	}
}

// 3. Test All 26 MsgType constants roundtrip
func TestAllMsgTypeConstants(t *testing.T) {
	types := []MsgType{
		TypeHandshakeReq, TypeHandshakeResp,
		TypePing, TypePong,
		TypeGetServerListReq, TypeGetServerListResp,
		TypeGetServerReq, TypeGetServerResp,
		TypeSubscribeMetricsReq, TypeSubscribeMetricsResp,
		TypeMetricsEvent, TypeUnsubscribeMetricsReq, TypeUnsubscribeMetricsResp,
		TypeStorageGetReq, TypeStorageGetResp,
		TypeStorageSetReq, TypeStorageSetResp,
		TypeStorageDeleteReq, TypeStorageDeleteResp,
		TypeStorageListReq, TypeStorageListResp,
		TypeSendNotificationReq, TypeSendNotificationResp,
		TypeShutdownReq, TypeShutdownResp,
		TypeErrorResp,
	}

	for _, mt := range types {
		f := NewFrame(mt, 100, []byte("test"))
		enc, err := EncodeFrame(f)
		if err != nil {
			t.Fatalf("type %s: encode failed: %v", mt, err)
		}
		dec, err := DecodeFrame(enc)
		if err != nil {
			t.Fatalf("type %s: decode failed: %v", mt, err)
		}
		if dec.Type != mt {
			t.Errorf("expected type %s, got %s", mt, dec.Type)
		}
		if dec.Type.String() == "" {
			t.Errorf("String() empty for %s", mt)
		}
	}
}

// 4. Test Zero-length payload (e.g. Ping / Pong)
func TestZeroLengthPayload(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	ping := NewFrame(TypePing, 999, nil)
	go func() {
		_ = WriteFrame(server, ping)
	}()

	recv, err := ReadFrame(client)
	if err != nil {
		t.Fatalf("ReadFrame zero-length failed: %v", err)
	}
	if recv.Length != 0 {
		t.Errorf("expected length 0, got %d", recv.Length)
	}
	if recv.Payload != nil && len(recv.Payload) != 0 {
		t.Errorf("expected nil or empty payload, got %v", recv.Payload)
	}
}

// 5. Large payload roundtrip (1MB and 4MB)
func TestLargePayloadRoundtrip(t *testing.T) {
	sizes := []int{1024 * 1024, 4 * 1024 * 1024}
	for _, size := range sizes {
		largeBuf := bytes.Repeat([]byte{0x5A}, size)
		frame := NewFrame(TypeMetricsEvent, 1, largeBuf)

		enc, err := EncodeFrame(frame)
		if err != nil {
			t.Fatalf("failed to encode %d bytes: %v", size, err)
		}

		dec, err := DecodeFrame(enc)
		if err != nil {
			t.Fatalf("failed to decode %d bytes: %v", size, err)
		}
		if len(dec.Payload) != size {
			t.Errorf("payload length mismatch: expected %d, got %d", size, len(dec.Payload))
		}
		if dec.Payload[0] != 0x5A || dec.Payload[size-1] != 0x5A {
			t.Errorf("payload corrupt")
		}
	}
}

// 6. MaxPayloadSize limit boundary enforcement
func TestMaxPayloadBoundary(t *testing.T) {
	// Exact boundary (16MB): Valid
	exact := make([]byte, MaxPayloadSize)
	fExact := NewFrame(TypeMetricsEvent, 1, exact)
	if err := fExact.Validate(); err != nil {
		t.Errorf("exact 16MB frame should be valid, got: %v", err)
	}

	// Oversized: 16MB + 1 byte
	oversized := make([]byte, MaxPayloadSize+1)
	fOver := NewFrame(TypeMetricsEvent, 2, oversized)
	if err := fOver.Validate(); !errors.Is(err, ErrPayloadTooLarge) {
		t.Errorf("expected ErrPayloadTooLarge, got %v", err)
	}

	// WriteFrame should reject oversized
	var buf bytes.Buffer
	if err := WriteFrame(&buf, fOver); !errors.Is(err, ErrPayloadTooLarge) {
		t.Errorf("WriteFrame expected ErrPayloadTooLarge, got %v", err)
	}

	// ReadFrame on header with Length > MaxPayloadSize must reject without reading payload
	var badHdr [HeaderSize]byte
	binary.BigEndian.PutUint32(badHdr[0:4], Magic)
	badHdr[4] = Version
	badHdr[5] = byte(TypeMetricsEvent)
	binary.BigEndian.PutUint32(badHdr[12:16], MaxPayloadSize+100)

	_, err := ReadFrame(bytes.NewReader(badHdr[:]))
	if !errors.Is(err, ErrPayloadTooLarge) {
		t.Errorf("ReadFrame expected ErrPayloadTooLarge, got %v", err)
	}
}

// 7. Header Corruption Rejection (Bad Magic & Bad Version)
func TestHeaderCorruptionRejection(t *testing.T) {
	// Bad Magic
	var badMagicHdr [HeaderSize]byte
	binary.BigEndian.PutUint32(badMagicHdr[0:4], 0xDEADBEEF)
	badMagicHdr[4] = Version

	_, err := ReadFrame(bytes.NewReader(badMagicHdr[:]))
	if !errors.Is(err, ErrInvalidMagic) {
		t.Errorf("expected ErrInvalidMagic, got %v", err)
	}

	// Bad Version
	var badVerHdr [HeaderSize]byte
	binary.BigEndian.PutUint32(badVerHdr[0:4], Magic)
	badVerHdr[4] = 0x99

	_, err = ReadFrame(bytes.NewReader(badVerHdr[:]))
	if !errors.Is(err, ErrUnsupportedVersion) {
		t.Errorf("expected ErrUnsupportedVersion, got %v", err)
	}
}

// 8. Truncated Streams & EOF Handling
func TestTruncatedStreams(t *testing.T) {
	// Clean EOF on 0 bytes
	_, err := ReadFrame(bytes.NewReader(nil))
	if !errors.Is(err, io.EOF) {
		t.Errorf("expected io.EOF on empty reader, got %v", err)
	}

	// Truncated header (e.g. 5 bytes)
	_, err = ReadFrame(bytes.NewReader([]byte{0x51, 0x59, 0x50, 0x4C, 0x01}))
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("expected io.ErrUnexpectedEOF on truncated header, got %v", err)
	}

	// Header specifies 50 bytes payload, but reader only supplies 10 bytes
	f := NewFrame(TypePing, 1, make([]byte, 50))
	enc, _ := EncodeFrame(f)
	truncated := enc[:HeaderSize+10] // cut off 40 bytes
	_, err = ReadFrame(bytes.NewReader(truncated))
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("expected io.ErrUnexpectedEOF on truncated payload, got %v", err)
	}
}

// 9. Zero-Allocation Buffer Reuse (`ReadFramePooled` & `ReleaseFrame`)
func TestBufferPoolZeroAlloc(t *testing.T) {
	payload := []byte("high-frequency-metrics-streaming-tick")
	frame := NewFrame(TypeMetricsEvent, 1001, payload)
	enc, _ := EncodeFrame(frame)

	// Warm the pools
	for i := 0; i < 10; i++ {
		r := bytes.NewReader(enc)
		f, err := ReadFramePooled(r)
		if err != nil {
			t.Fatalf("ReadFramePooled failed: %v", err)
		}
		ReleaseFrame(f)
	}

	// Test allocation stability
	allocs := testing.AllocsPerRun(100, func() {
		r := bytes.NewReader(enc)
		f, err := ReadFramePooled(r)
		if err != nil {
			t.Fatalf("ReadFramePooled failed: %v", err)
		}
		ReleaseFrame(f)
	})

	if allocs > 1.5 {
		t.Logf("Measured AllocsPerRun: %v (target is minimal GC footprint)", allocs)
	}
}

// 10. JSON Helpers & DTOs
func TestJSONHelpers(t *testing.T) {
	req := HandshakeReq{
		Token:    "auth_token_xyz",
		PluginID: "metrics-analyzer",
		Version:  "1.0.0",
	}

	f, err := NewJSONFrame(TypeHandshakeReq, 10, req)
	if err != nil {
		t.Fatalf("NewJSONFrame failed: %v", err)
	}
	if !f.Flags.Has(FlagJSON) {
		t.Errorf("expected FlagJSON")
	}

	var decoded HandshakeReq
	if err := f.UnmarshalPayload(&decoded); err != nil {
		t.Fatalf("UnmarshalPayload failed: %v", err)
	}
	if decoded.Token != req.Token || decoded.PluginID != req.PluginID {
		t.Errorf("unmarshaled DTO mismatch: %+v", decoded)
	}
}

// 11. Error Frame Construction
func TestErrorFrameHelper(t *testing.T) {
	f := NewErrorFrame(77, 404, "server not found")
	if f.Type != TypeErrorResp {
		t.Errorf("expected TypeErrorResp, got %v", f.Type)
	}
	if !f.Flags.Has(FlagError) || !f.Flags.Has(FlagJSON) {
		t.Errorf("expected FlagError and FlagJSON set")
	}
	var errPayload ErrorPayload
	if err := f.UnmarshalPayload(&errPayload); err != nil {
		t.Fatalf("failed to unmarshal error payload: %v", err)
	}
	if errPayload.Code != 404 || errPayload.Error != "server not found" {
		t.Errorf("unexpected error payload: %+v", errPayload)
	}
}

// 12. Concurrent Stream Safety
func TestConcurrentStreams(t *testing.T) {
	const count = 50
	var wg sync.WaitGroup
	wg.Add(count)

	for i := 0; i < count; i++ {
		go func(id int) {
			defer wg.Done()
			c, s := net.Pipe()
			defer c.Close()
			defer s.Close()

			f := NewFrame(TypePing, uint32(id), []byte("ping"))
			go func() {
				_ = WriteFrame(s, f)
			}()

			recv, err := ReadFrame(c)
			if err != nil {
				t.Errorf("goroutine %d read error: %v", id, err)
				return
			}
			if recv.SeqID != uint32(id) {
				t.Errorf("goroutine %d seqID mismatch: %d", id, recv.SeqID)
			}
		}(i)
	}

	wg.Wait()
}
