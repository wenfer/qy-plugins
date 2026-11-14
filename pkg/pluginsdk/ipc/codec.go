package ipc

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
)

// ReadFrame reads a complete frame from an io.Reader.
// Payload is allocated fresh. Returns io.EOF if stream closed cleanly with 0 bytes read,
// or io.ErrUnexpectedEOF if stream cuts off mid-frame.
func ReadFrame(r io.Reader) (*Frame, error) {
	var hdrBuf [HeaderSize]byte
	if _, err := io.ReadFull(r, hdrBuf[:]); err != nil {
		return nil, err
	}

	magic := binary.BigEndian.Uint32(hdrBuf[0:4])
	if magic != Magic {
		return nil, fmt.Errorf("%w: expected 0x%08X, got 0x%08X", ErrInvalidMagic, Magic, magic)
	}

	version := hdrBuf[4]
	if version != Version {
		return nil, fmt.Errorf("%w: expected 0x%02X, got 0x%02X", ErrUnsupportedVersion, Version, version)
	}

	msgType := MsgType(hdrBuf[5])
	flags := Flag(hdrBuf[6])
	reserved := hdrBuf[7]
	seqID := binary.BigEndian.Uint32(hdrBuf[8:12])
	length := binary.BigEndian.Uint32(hdrBuf[12:16])

	if length > MaxPayloadSize {
		return nil, fmt.Errorf("%w: length %d exceeds max %d", ErrPayloadTooLarge, length, MaxPayloadSize)
	}

	var payload []byte
	if length > 0 {
		payload = make([]byte, length)
		if _, err := io.ReadFull(r, payload); err != nil {
			if errors.Is(err, io.EOF) {
				return nil, io.ErrUnexpectedEOF
			}
			return nil, err
		}
	}

	return &Frame{
		Magic:    magic,
		Version:  version,
		Type:     msgType,
		Flags:    flags,
		Reserved: reserved,
		SeqID:    seqID,
		Length:   length,
		Payload:  payload,
	}, nil
}

// ReadFramePooled reads a frame using pooled Frame and byte buffer resources.
// The caller MUST call ReleaseFrame(f) when processing is complete.
func ReadFramePooled(r io.Reader) (*Frame, error) {
	hdrBuf := acquireHeaderBuf()
	defer releaseHeaderBuf(hdrBuf)

	if _, err := io.ReadFull(r, *hdrBuf); err != nil {
		return nil, err
	}

	hdr := *hdrBuf
	magic := binary.BigEndian.Uint32(hdr[0:4])
	if magic != Magic {
		return nil, fmt.Errorf("%w: expected 0x%08X, got 0x%08X", ErrInvalidMagic, Magic, magic)
	}

	version := hdr[4]
	if version != Version {
		return nil, fmt.Errorf("%w: expected 0x%02X, got 0x%02X", ErrUnsupportedVersion, Version, version)
	}

	msgType := MsgType(hdr[5])
	flags := Flag(hdr[6])
	reserved := hdr[7]
	seqID := binary.BigEndian.Uint32(hdr[8:12])
	length := binary.BigEndian.Uint32(hdr[12:16])

	if length > MaxPayloadSize {
		return nil, fmt.Errorf("%w: length %d exceeds max %d", ErrPayloadTooLarge, length, MaxPayloadSize)
	}

	f := AcquireFrame()
	f.Magic = magic
	f.Version = version
	f.Type = msgType
	f.Flags = flags
	f.Reserved = reserved
	f.SeqID = seqID
	f.Length = length

	if length > 0 {
		buf := GetBuffer(int(length))
		if _, err := io.ReadFull(r, buf); err != nil {
			PutBuffer(buf)
			ReleaseFrame(f)
			if errors.Is(err, io.EOF) {
				return nil, io.ErrUnexpectedEOF
			}
			return nil, err
		}
		f.Payload = buf
	} else {
		f.Payload = nil
	}

	return f, nil
}

// WriteFrame writes a frame to an io.Writer.
// If w is a net.Conn or supports vectorized I/O, net.Buffers performs a zero-copy writev.
func WriteFrame(w io.Writer, f *Frame) error {
	if f == nil {
		return ErrNilFrame
	}

	payloadLen := len(f.Payload)
	if payloadLen > MaxPayloadSize {
		return fmt.Errorf("%w: length %d exceeds max %d", ErrPayloadTooLarge, payloadLen, MaxPayloadSize)
	}

	magic := f.Magic
	if magic == 0 {
		magic = Magic
	}
	version := f.Version
	if version == 0 {
		version = Version
	}

	hdrBuf := acquireHeaderBuf()
	defer releaseHeaderBuf(hdrBuf)

	hdr := *hdrBuf
	binary.BigEndian.PutUint32(hdr[0:4], magic)
	hdr[4] = version
	hdr[5] = byte(f.Type)
	hdr[6] = byte(f.Flags)
	hdr[7] = f.Reserved
	binary.BigEndian.PutUint32(hdr[8:12], f.SeqID)
	binary.BigEndian.PutUint32(hdr[12:16], uint32(payloadLen))

	if payloadLen == 0 {
		_, err := w.Write(hdr)
		return err
	}

	bufs := net.Buffers{hdr, f.Payload}
	_, err := bufs.WriteTo(w)
	return err
}

// EncodeFrame serializes a Frame into a single continuous byte slice.
func EncodeFrame(f *Frame) ([]byte, error) {
	if f == nil {
		return nil, ErrNilFrame
	}
	payloadLen := len(f.Payload)
	if payloadLen > MaxPayloadSize {
		return nil, fmt.Errorf("%w: length %d exceeds max %d", ErrPayloadTooLarge, payloadLen, MaxPayloadSize)
	}

	magic := f.Magic
	if magic == 0 {
		magic = Magic
	}
	version := f.Version
	if version == 0 {
		version = Version
	}

	buf := make([]byte, HeaderSize+payloadLen)
	binary.BigEndian.PutUint32(buf[0:4], magic)
	buf[4] = version
	buf[5] = byte(f.Type)
	buf[6] = byte(f.Flags)
	buf[7] = f.Reserved
	binary.BigEndian.PutUint32(buf[8:12], f.SeqID)
	binary.BigEndian.PutUint32(buf[12:16], uint32(payloadLen))

	if payloadLen > 0 {
		copy(buf[HeaderSize:], f.Payload)
	}
	return buf, nil
}

// DecodeFrame parses a continuous byte slice into a Frame.
func DecodeFrame(data []byte) (*Frame, error) {
	if len(data) < HeaderSize {
		return nil, ErrIncompleteFrame
	}

	magic := binary.BigEndian.Uint32(data[0:4])
	if magic != Magic {
		return nil, fmt.Errorf("%w: expected 0x%08X, got 0x%08X", ErrInvalidMagic, Magic, magic)
	}

	version := data[4]
	if version != Version {
		return nil, fmt.Errorf("%w: expected 0x%02X, got 0x%02X", ErrUnsupportedVersion, Version, version)
	}

	msgType := MsgType(data[5])
	flags := Flag(data[6])
	reserved := data[7]
	seqID := binary.BigEndian.Uint32(data[8:12])
	length := binary.BigEndian.Uint32(data[12:16])

	if length > MaxPayloadSize {
		return nil, fmt.Errorf("%w: length %d exceeds max %d", ErrPayloadTooLarge, length, MaxPayloadSize)
	}

	if uint32(len(data)-HeaderSize) < length {
		return nil, ErrIncompleteFrame
	}

	var payload []byte
	if length > 0 {
		payload = make([]byte, length)
		copy(payload, data[HeaderSize:HeaderSize+length])
	}

	return &Frame{
		Magic:    magic,
		Version:  version,
		Type:     msgType,
		Flags:    flags,
		Reserved: reserved,
		SeqID:    seqID,
		Length:   length,
		Payload:  payload,
	}, nil
}
