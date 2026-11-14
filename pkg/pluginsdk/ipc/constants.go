package ipc

import "fmt"

// Protocol Framing Constants
const (
	Magic          uint32 = 0x5159504C       // "QYPL" in ASCII
	Version        uint8  = 0x01             // Protocol version 1
	HeaderSize            = 16               // Fixed binary header size in bytes
	MaxPayloadSize        = 16 * 1024 * 1024 // 16 MB maximum payload guard
)

// MsgType defines the 1-byte message type identifier.
type MsgType uint8

const (
	// Handshake
	TypeHandshakeReq  MsgType = 0x01 // Plugin -> Host: authentication & metadata
	TypeHandshakeResp MsgType = 0x02 // Host -> Plugin: auth status & host capabilities

	// Heartbeat
	TypePing MsgType = 0x03 // Keepalive ping
	TypePong MsgType = 0x04 // Keepalive pong

	// Server Query RPC
	TypeGetServerListReq  MsgType = 0x10 // Plugin -> Host: query all servers with stats
	TypeGetServerListResp MsgType = 0x11 // Host -> Plugin: list of servers
	TypeGetServerReq      MsgType = 0x12 // Plugin -> Host: query specific server
	TypeGetServerResp     MsgType = 0x13 // Host -> Plugin: server detail

	// Metrics Streaming Pub/Sub
	TypeSubscribeMetricsReq    MsgType = 0x20 // Plugin -> Host: request real-time metrics feed
	TypeSubscribeMetricsResp   MsgType = 0x21 // Host -> Plugin: subscription confirmed
	TypeMetricsEvent           MsgType = 0x22 // Host -> Plugin: real-time ServerWithStats stream event
	TypeUnsubscribeMetricsReq  MsgType = 0x23 // Plugin -> Host: stop metrics feed
	TypeUnsubscribeMetricsResp MsgType = 0x24 // Host -> Plugin: unsubscription confirmed

	// Isolated Key-Value Storage
	TypeStorageGetReq     MsgType = 0x30 // Plugin -> Host: get KV value by key
	TypeStorageGetResp    MsgType = 0x31 // Host -> Plugin: KV value result
	TypeStorageSetReq     MsgType = 0x32 // Plugin -> Host: set KV key-value
	TypeStorageSetResp    MsgType = 0x33 // Host -> Plugin: set result
	TypeStorageDeleteReq  MsgType = 0x34 // Plugin -> Host: delete KV key
	TypeStorageDeleteResp MsgType = 0x35 // Host -> Plugin: delete result
	TypeStorageListReq    MsgType = 0x36 // Plugin -> Host: list keys by prefix
	TypeStorageListResp   MsgType = 0x37 // Host -> Plugin: list result

	// Notification Dispatch
	TypeSendNotificationReq  MsgType = 0x40 // Plugin -> Host: dispatch notification alert
	TypeSendNotificationResp MsgType = 0x41 // Host -> Plugin: dispatch result

	// Lifecycle & Process Management
	TypeShutdownReq  MsgType = 0x50 // Host -> Plugin: request clean shutdown
	TypeShutdownResp MsgType = 0x51 // Plugin -> Host: shutdown acknowledge

	// Generic Error Response
	TypeErrorResp MsgType = 0xF0 // General error response
)

// String returns human-readable representation of MsgType.
func (t MsgType) String() string {
	switch t {
	case TypeHandshakeReq:
		return "HandshakeReq"
	case TypeHandshakeResp:
		return "HandshakeResp"
	case TypePing:
		return "Ping"
	case TypePong:
		return "Pong"
	case TypeGetServerListReq:
		return "GetServerListReq"
	case TypeGetServerListResp:
		return "GetServerListResp"
	case TypeGetServerReq:
		return "GetServerReq"
	case TypeGetServerResp:
		return "GetServerResp"
	case TypeSubscribeMetricsReq:
		return "SubscribeMetricsReq"
	case TypeSubscribeMetricsResp:
		return "SubscribeMetricsResp"
	case TypeMetricsEvent:
		return "MetricsEvent"
	case TypeUnsubscribeMetricsReq:
		return "UnsubscribeMetricsReq"
	case TypeUnsubscribeMetricsResp:
		return "UnsubscribeMetricsResp"
	case TypeStorageGetReq:
		return "StorageGetReq"
	case TypeStorageGetResp:
		return "StorageGetResp"
	case TypeStorageSetReq:
		return "StorageSetReq"
	case TypeStorageSetResp:
		return "StorageSetResp"
	case TypeStorageDeleteReq:
		return "StorageDeleteReq"
	case TypeStorageDeleteResp:
		return "StorageDeleteResp"
	case TypeStorageListReq:
		return "StorageListReq"
	case TypeStorageListResp:
		return "StorageListResp"
	case TypeSendNotificationReq:
		return "SendNotificationReq"
	case TypeSendNotificationResp:
		return "SendNotificationResp"
	case TypeShutdownReq:
		return "ShutdownReq"
	case TypeShutdownResp:
		return "ShutdownResp"
	case TypeErrorResp:
		return "ErrorResp"
	default:
		return fmt.Sprintf("MsgType(0x%02X)", byte(t))
	}
}

// Flag defines bit flags for the 1-byte Flags header field.
type Flag uint8

const (
	FlagNone       Flag = 0x00
	FlagJSON       Flag = 0x01 // Payload contains JSON formatted text
	FlagCompressed Flag = 0x02 // Payload is compressed
	FlagError      Flag = 0x04 // Message indicates an application error
)

func (f Flag) Has(flag Flag) bool {
	return f&flag != 0
}

func (f *Flag) Set(flag Flag) {
	*f |= flag
}
