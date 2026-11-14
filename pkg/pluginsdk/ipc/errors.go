package ipc

import "errors"

var (
	ErrInvalidMagic       = errors.New("ipc: invalid magic number")
	ErrUnsupportedVersion = errors.New("ipc: unsupported protocol version")
	ErrPayloadTooLarge    = errors.New("ipc: payload exceeds maximum allowed size (16MB)")
	ErrInvalidHeader      = errors.New("ipc: invalid frame header")
	ErrIncompleteFrame    = errors.New("ipc: incomplete frame data")
	ErrNilFrame           = errors.New("ipc: cannot process nil frame")
)
