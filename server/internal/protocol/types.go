// Package protocol defines the wire protocol types and codec shared between
// the Go server and Rust agent.
package protocol

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/google/uuid"
)

// DeviceID uniquely identifies a device/agent.
type DeviceID = uuid.UUID

// GroupID uniquely identifies a device group.
type GroupID = uuid.UUID

// SessionToken is a 32-byte random hex string used for relay session routing.
type SessionToken string

// GenerateSessionToken creates a new random session token (64 hex chars).
func GenerateSessionToken() SessionToken {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return SessionToken(hex.EncodeToString(buf[:]))
}

// AgentCapability represents a feature the agent supports.
type AgentCapability string

const (
	CapRemoteDesktop AgentCapability = "RemoteDesktop"
	CapTerminal      AgentCapability = "Terminal"
	CapFileManager   AgentCapability = "FileManager"
	CapInput         AgentCapability = "InputInjection"
	CapProcess       AgentCapability = "ProcessManager"
)

// DeviceStatus represents the current connection state of a device.
type DeviceStatus string

const (
	StatusOnline     DeviceStatus = "Online"
	StatusOffline    DeviceStatus = "Offline"
	StatusConnecting DeviceStatus = "Connecting"
)

// Permissions defines what a session is allowed to do.
type Permissions struct {
	Desktop   bool `msgpack:"desktop" json:"desktop"`
	Terminal  bool `msgpack:"terminal" json:"terminal"`
	FileRead  bool `msgpack:"file_read" json:"file_read"`
	FileWrite bool `msgpack:"file_write" json:"file_write"`
	Input     bool `msgpack:"input" json:"input"`
}

// FrameEncoding specifies the compression format for desktop frames.
type FrameEncoding string

const (
	EncodingRaw       FrameEncoding = "Raw"
	EncodingZlib      FrameEncoding = "Zlib"
	EncodingZstd      FrameEncoding = "Zstd"
	EncodingH264Idr   FrameEncoding = "H264Idr"
	EncodingH264Delta FrameEncoding = "H264Delta"
)

// DesktopFrame contains compressed pixel data for a screen region.
type DesktopFrame struct {
	Sequence uint64        `msgpack:"sequence"`
	X        uint16        `msgpack:"x"`
	Y        uint16        `msgpack:"y"`
	Width    uint16        `msgpack:"width"`
	Height   uint16        `msgpack:"height"`
	Encoding FrameEncoding `msgpack:"encoding"`
	Data     []byte        `msgpack:"data"`
}

// TerminalFrame contains terminal output data.
type TerminalFrame struct {
	Data []byte `msgpack:"data"`
}

// FileFrame contains a chunk of file transfer data.
type FileFrame struct {
	Offset    uint64 `msgpack:"offset"`
	TotalSize uint64 `msgpack:"total_size"`
	Data      []byte `msgpack:"data"`
}

// FrameType constants matching the Rust wire protocol.
const (
	FrameControl  byte = 0x01
	FrameDesktop  byte = 0x02
	FrameTerminal byte = 0x03
	FrameFile     byte = 0x04
	FramePing     byte = 0x05
	FramePong     byte = 0x06
)

// HandshakeMessageType constants for binary handshake encoding.
const (
	MsgServerHello byte = 0x10
	MsgAgentHello  byte = 0x11
	MsgServerProof byte = 0x12
	MsgAgentProof  byte = 0x13
	MsgSkipAuth    byte = 0x14
	MsgExpectHash  byte = 0x15
)

// FileEntry represents a file or directory in a listing.
type FileEntry struct {
	Name     string `msgpack:"name" json:"name"`
	IsDir    bool   `msgpack:"is_dir" json:"is_dir"`
	Size     uint64 `msgpack:"size" json:"size"`
	Modified int64  `msgpack:"modified" json:"modified"`
}

// MaxFrameSize is the maximum payload size for a single frame (16 MiB).
const MaxFrameSize = 16 * 1024 * 1024

// RedactToken returns the first 8 characters of a token for safe logging.
func RedactToken(token string) string {
	if len(token) <= 8 {
		return "***"
	}
	return token[:8] + "..."
}
