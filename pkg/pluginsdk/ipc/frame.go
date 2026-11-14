package ipc

import (
	"encoding/json"
	"fmt"
)

// Frame represents a single IPC packet in memory.
type Frame struct {
	Magic    uint32
	Version  uint8
	Type     MsgType
	Flags    Flag
	Reserved uint8
	SeqID    uint32
	Length   uint32
	Payload  []byte
}

// NewFrame constructs a standard Frame with default Magic and Version.
func NewFrame(msgType MsgType, seqID uint32, payload []byte) *Frame {
	return &Frame{
		Magic:   Magic,
		Version: Version,
		Type:    msgType,
		SeqID:   seqID,
		Length:  uint32(len(payload)),
		Payload: payload,
	}
}

// NewJSONFrame constructs a Frame with a JSON-serialized payload and FlagJSON set.
func NewJSONFrame(msgType MsgType, seqID uint32, v any) (*Frame, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("ipc: marshal json payload: %w", err)
	}
	f := NewFrame(msgType, seqID, data)
	f.Flags.Set(FlagJSON)
	return f, nil
}

// NewErrorFrame constructs an ErrorResp frame carrying an ErrorPayload with FlagError and FlagJSON set.
func NewErrorFrame(seqID uint32, code int, errMsg string) *Frame {
	payload, _ := json.Marshal(ErrorPayload{Code: code, Error: errMsg})
	f := NewFrame(TypeErrorResp, seqID, payload)
	f.Flags.Set(FlagJSON | FlagError)
	return f
}

// UnmarshalPayload deserializes the Frame's Payload into the provided value.
func (f *Frame) UnmarshalPayload(v any) error {
	if len(f.Payload) == 0 {
		return nil
	}
	return json.Unmarshal(f.Payload, v)
}

// Reset clears the frame fields for sync.Pool reuse.
func (f *Frame) Reset() {
	f.Magic = 0
	f.Version = 0
	f.Type = 0
	f.Flags = 0
	f.Reserved = 0
	f.SeqID = 0
	f.Length = 0
	f.Payload = nil
}

// Validate verifies the frame's integrity and boundary limits.
func (f *Frame) Validate() error {
	if f.Magic != Magic {
		return fmt.Errorf("%w: got 0x%08X", ErrInvalidMagic, f.Magic)
	}
	if f.Version != Version {
		return fmt.Errorf("%w: got 0x%02X", ErrUnsupportedVersion, f.Version)
	}
	if f.Length > MaxPayloadSize {
		return fmt.Errorf("%w: length %d exceeds max %d", ErrPayloadTooLarge, f.Length, MaxPayloadSize)
	}
	if uint32(len(f.Payload)) != f.Length {
		return fmt.Errorf("%w: payload len %d != header length %d", ErrInvalidHeader, len(f.Payload), f.Length)
	}
	return nil
}
