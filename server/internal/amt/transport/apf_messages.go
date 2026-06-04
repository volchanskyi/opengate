package transport

import (
	"encoding/binary"
	"fmt"

	"github.com/google/uuid"
)

// This file holds the typed APF message structs and their parsers, plus the
// GUID/forward-data helpers. The wire framing and body readers live in apf.go;
// the message writers in apf_write.go.

// ServiceRequest represents an APF service request.
type ServiceRequest struct {
	ServiceName string
}

// ParseServiceRequest parses a ServiceRequest from payload bytes.
func ParseServiceRequest(data []byte) (ServiceRequest, error) {
	name, _, err := readString(data, 0)
	if err != nil {
		return ServiceRequest{}, err
	}
	return ServiceRequest{ServiceName: name}, nil
}

// UserAuthRequest represents an APF user authentication request.
type UserAuthRequest struct {
	Username    string
	ServiceName string
	MethodName  string
}

// ParseUserAuthRequest parses a UserAuthRequest from payload bytes.
func ParseUserAuthRequest(data []byte) (UserAuthRequest, error) {
	username, off, err := readString(data, 0)
	if err != nil {
		return UserAuthRequest{}, fmt.Errorf("username: %w", err)
	}
	service, off, err := readString(data, off)
	if err != nil {
		return UserAuthRequest{}, fmt.Errorf("service: %w", err)
	}
	method, _, err := readString(data, off)
	if err != nil {
		return UserAuthRequest{}, fmt.Errorf("method: %w", err)
	}
	return UserAuthRequest{
		Username:    username,
		ServiceName: service,
		MethodName:  method,
	}, nil
}

// GlobalRequest represents an APF global request (e.g., TCP forward).
type GlobalRequest struct {
	RequestName string
	WantReply   bool
	Data        []byte
}

// ParseGlobalRequest parses a GlobalRequest from payload bytes.
func ParseGlobalRequest(data []byte) (GlobalRequest, error) {
	name, off, err := readString(data, 0)
	if err != nil {
		return GlobalRequest{}, fmt.Errorf("request name: %w", err)
	}
	if off >= len(data) {
		return GlobalRequest{}, ErrMessageTooShort
	}
	wantReply := data[off] != 0
	off++
	return GlobalRequest{
		RequestName: name,
		WantReply:   wantReply,
		Data:        data[off:],
	}, nil
}

// ChannelOpen represents an APF channel open request.
type ChannelOpen struct {
	ChannelType     string
	SenderChannel   uint32
	InitialWindowSz uint32
	MaxPacketSz     uint32
	Data            []byte // connected address, port, origin, etc.
}

// ParseChannelOpen parses a ChannelOpen from payload bytes.
func ParseChannelOpen(data []byte) (ChannelOpen, error) {
	chType, off, err := readString(data, 0)
	if err != nil {
		return ChannelOpen{}, fmt.Errorf("channel type: %w", err)
	}
	if off+12 > len(data) {
		return ChannelOpen{}, ErrMessageTooShort
	}
	sender := binary.BigEndian.Uint32(data[off:])
	window := binary.BigEndian.Uint32(data[off+4:])
	maxPkt := binary.BigEndian.Uint32(data[off+8:])
	off += 12
	return ChannelOpen{
		ChannelType:     chType,
		SenderChannel:   sender,
		InitialWindowSz: window,
		MaxPacketSz:     maxPkt,
		Data:            data[off:],
	}, nil
}

// ChannelData represents data on an open APF channel.
type ChannelData struct {
	RecipientChannel uint32
	Data             []byte
}

// ParseChannelData parses ChannelData from payload bytes.
func ParseChannelData(data []byte) (ChannelData, error) {
	if len(data) < 4 {
		return ChannelData{}, ErrMessageTooShort
	}
	ch := binary.BigEndian.Uint32(data[:4])
	str, _, err := readString(data, 4)
	if err != nil {
		return ChannelData{}, fmt.Errorf("data payload: %w", err)
	}
	return ChannelData{
		RecipientChannel: ch,
		Data:             []byte(str),
	}, nil
}

// ProtocolVersion represents the APF protocol version message.
type ProtocolVersion struct {
	MajorVersion uint32
	MinorVersion uint32
	Trigger      uint32
	UUID         [16]byte
}

// ParseProtocolVersion parses a ProtocolVersion from payload bytes.
func ParseProtocolVersion(data []byte) (ProtocolVersion, error) {
	if len(data) < 28 {
		return ProtocolVersion{}, ErrMessageTooShort
	}
	pv := ProtocolVersion{
		MajorVersion: binary.BigEndian.Uint32(data[0:4]),
		MinorVersion: binary.BigEndian.Uint32(data[4:8]),
		Trigger:      binary.BigEndian.Uint32(data[8:12]),
	}
	copy(pv.UUID[:], data[12:28])
	return pv, nil
}

// KeepaliveRequest represents an APF keepalive request or reply (same wire format).
type KeepaliveRequest struct {
	Cookie uint32
}

// ParseKeepaliveRequest parses a KeepaliveRequest from payload bytes.
func ParseKeepaliveRequest(data []byte) (KeepaliveRequest, error) {
	if len(data) < 4 {
		return KeepaliveRequest{}, ErrMessageTooShort
	}
	return KeepaliveRequest{Cookie: binary.BigEndian.Uint32(data)}, nil
}

// KeepaliveOptions represents keepalive interval/timeout negotiation.
type KeepaliveOptions struct {
	Interval uint32
	Timeout  uint32
}

// ParseKeepaliveOptions parses KeepaliveOptions from payload bytes.
func ParseKeepaliveOptions(data []byte) (KeepaliveOptions, error) {
	if len(data) < 8 {
		return KeepaliveOptions{}, ErrMessageTooShort
	}
	return KeepaliveOptions{
		Interval: binary.BigEndian.Uint32(data[0:4]),
		Timeout:  binary.BigEndian.Uint32(data[4:8]),
	}, nil
}

// ReorderIntelGUID applies Intel's mixed-endian byte reordering to a raw
// 16-byte GUID from the ProtocolVersion message and returns a standard UUID.
// Intel format: first 3 groups little-endian, last 2 groups big-endian.
func ReorderIntelGUID(raw [16]byte) uuid.UUID {
	var u uuid.UUID
	// Group 1 (4 bytes): LE → BE
	u[0], u[1], u[2], u[3] = raw[3], raw[2], raw[1], raw[0]
	// Group 2 (2 bytes): LE → BE
	u[4], u[5] = raw[5], raw[4]
	// Group 3 (2 bytes): LE → BE
	u[6], u[7] = raw[7], raw[6]
	// Groups 4-5 (8 bytes): already BE
	copy(u[8:], raw[8:16])
	return u
}

// ParseForwardData parses the address and port from a tcpip-forward GlobalRequest's Data field.
func ParseForwardData(data []byte) (string, uint32, error) {
	addr, off, err := readString(data, 0)
	if err != nil {
		return "", 0, fmt.Errorf("forward address: %w", err)
	}
	if off+4 > len(data) {
		return "", 0, fmt.Errorf("forward port: %w", ErrMessageTooShort)
	}
	port := binary.BigEndian.Uint32(data[off:])
	return addr, port, nil
}
