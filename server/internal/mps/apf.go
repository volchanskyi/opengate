// Package mps implements the Intel AMT Management Presence Server.
//
// The MPS accepts CIRA (Client Initiated Remote Access) connections from
// Intel AMT devices over TLS. Communication uses the APF (AMT Port
// Forwarding) protocol, which is based on SSH channel semantics defined
// in RFC 4254 with Intel extensions.
package mps

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/google/uuid"
)

// APF message type constants per Intel AMT CIRA specification.
const (
	APFDisconnect         uint8 = 1
	APFServiceRequest     uint8 = 5
	APFServiceAccept      uint8 = 6
	APFUserAuthRequest    uint8 = 50
	APFUserAuthSuccess    uint8 = 52
	APFUserAuthFailure    uint8 = 51
	APFGlobalRequest      uint8 = 80
	APFRequestSuccess     uint8 = 81
	APFRequestFailure     uint8 = 82
	APFChannelOpen        uint8 = 90
	APFChannelOpenConfirm uint8 = 91
	APFChannelOpenFailure uint8 = 92
	APFChannelWindowAdj   uint8 = 93
	APFChannelData        uint8 = 94
	APFChannelClose       uint8 = 97
	APFProtocolVersion          uint8 = 192
	APFKeepaliveRequest         uint8 = 208
	APFKeepaliveReply           uint8 = 209
	APFKeepaliveOptionsRequest  uint8 = 210
	APFKeepaliveOptionsReply    uint8 = 211
)

// APF disconnect reason codes.
const (
	APFDisconnectByApp             uint32 = 11
	APFDisconnectProtocolError     uint32 = 2
	APFDisconnectServiceNotAvail   uint32 = 7
)

// Well-known APF service names.
const (
	ServiceAuth = "auth@amt.intel.com"
	ServicePFwd = "pfwd@amt.intel.com"
)

// Default APF window and max packet sizes.
const (
	DefaultWindowSize    uint32 = 0x8000 // 32 KiB (matches MeshCentral)
	DefaultMaxPacketSize uint32 = 0x8000
)

// maxAPFStringLen is the maximum allowed length for APF length-prefixed strings.
const maxAPFStringLen = 256

// ErrMessageTooShort is returned when a message is shorter than expected.
var ErrMessageTooShort = errors.New("apf: message too short")

// ErrUnknownMessageType is returned for unrecognised APF message types.
var ErrUnknownMessageType = errors.New("apf: unknown message type")

// --- Reading helpers ---

// ReadMessage reads one APF message from r and returns the type byte and payload.
func ReadMessage(r io.Reader) (uint8, []byte, error) {
	var msgType [1]byte
	if _, err := io.ReadFull(r, msgType[:]); err != nil {
		return 0, nil, err
	}

	payload, err := readMessageBody(r, msgType[0])
	if err != nil {
		return 0, nil, fmt.Errorf("apf type %d: %w", msgType[0], err)
	}
	return msgType[0], payload, nil
}

// readMessageBody reads the variable-length body for a given APF message type.
func readMessageBody(r io.Reader, msgType uint8) ([]byte, error) {
	switch msgType {
	case APFServiceRequest, APFServiceAccept:
		return readStringMsg(r)
	case APFUserAuthRequest:
		return readUserAuthRequest(r)
	case APFGlobalRequest:
		return readGlobalRequest(r)
	case APFChannelOpen:
		return readChannelOpen(r)
	case APFChannelData:
		return readChannelData(r)
	case APFChannelClose:
		return readFixed(r, 4) // recipient channel (uint32)
	case APFChannelWindowAdj:
		return readFixed(r, 8) // recipient channel + bytes to add
	case APFProtocolVersion:
		return readProtocolVersion(r)
	case APFKeepaliveRequest, APFKeepaliveReply:
		return readFixed(r, 4) // cookie
	case APFKeepaliveOptionsRequest, APFKeepaliveOptionsReply:
		return readFixed(r, 8) // interval + timeout
	case APFDisconnect:
		return readDisconnect(r)
	case APFUserAuthSuccess, APFUserAuthFailure,
		APFRequestSuccess, APFRequestFailure:
		return nil, nil // no body
	case APFChannelOpenConfirm:
		return readFixed(r, 16) // recipient + sender + window + max packet
	case APFChannelOpenFailure:
		return readFixed(r, 8) // recipient + reason
	default:
		return nil, fmt.Errorf("%w: %d", ErrUnknownMessageType, msgType)
	}
}

// --- Parsing types ---

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

// --- Writing helpers ---

// WriteServiceAccept writes an APF service accept message.
func WriteServiceAccept(w io.Writer, serviceName string) error {
	return writeStringMsg(w, APFServiceAccept, serviceName)
}

// WriteUserAuthSuccess writes an APF user auth success message.
func WriteUserAuthSuccess(w io.Writer) error {
	_, err := w.Write([]byte{APFUserAuthSuccess})
	return err
}

// WriteRequestSuccess writes an APF global request success response.
func WriteRequestSuccess(w io.Writer) error {
	_, err := w.Write([]byte{APFRequestSuccess})
	return err
}

// WriteChannelOpenConfirm writes a channel open confirmation.
func WriteChannelOpenConfirm(w io.Writer, recipientCh, senderCh, windowSz, maxPacket uint32) error {
	buf := make([]byte, 17)
	buf[0] = APFChannelOpenConfirm
	binary.BigEndian.PutUint32(buf[1:], recipientCh)
	binary.BigEndian.PutUint32(buf[5:], senderCh)
	binary.BigEndian.PutUint32(buf[9:], windowSz)
	binary.BigEndian.PutUint32(buf[13:], maxPacket)
	_, err := w.Write(buf)
	return err
}

// WriteChannelData writes channel data.
func WriteChannelData(w io.Writer, recipientCh uint32, data []byte) error {
	buf := make([]byte, 9+len(data))
	buf[0] = APFChannelData
	binary.BigEndian.PutUint32(buf[1:], recipientCh)
	binary.BigEndian.PutUint32(buf[5:], uint32(len(data)))
	copy(buf[9:], data)
	_, err := w.Write(buf)
	return err
}

// WriteChannelClose writes a channel close message.
func WriteChannelClose(w io.Writer, recipientCh uint32) error {
	buf := make([]byte, 5)
	buf[0] = APFChannelClose
	binary.BigEndian.PutUint32(buf[1:], recipientCh)
	_, err := w.Write(buf)
	return err
}

// WriteChannelWindowAdj writes a window adjust message.
func WriteChannelWindowAdj(w io.Writer, recipientCh, bytesToAdd uint32) error {
	buf := make([]byte, 9)
	buf[0] = APFChannelWindowAdj
	binary.BigEndian.PutUint32(buf[1:], recipientCh)
	binary.BigEndian.PutUint32(buf[5:], bytesToAdd)
	_, err := w.Write(buf)
	return err
}

// WriteDisconnect writes an APF disconnect message.
func WriteDisconnect(w io.Writer, reasonCode uint32) error {
	buf := make([]byte, 5)
	buf[0] = APFDisconnect
	binary.BigEndian.PutUint32(buf[1:], reasonCode)
	_, err := w.Write(buf)
	return err
}

// WriteProtocolVersion writes a protocol version message with a zero UUID.
func WriteProtocolVersion(w io.Writer, major, minor, trigger uint32) error {
	buf := make([]byte, 29) // 1 type + 4 major + 4 minor + 4 trigger + 16 UUID
	buf[0] = APFProtocolVersion
	binary.BigEndian.PutUint32(buf[1:], major)
	binary.BigEndian.PutUint32(buf[5:], minor)
	binary.BigEndian.PutUint32(buf[9:], trigger)
	// UUID bytes 13..28 are zero
	_, err := w.Write(buf)
	return err
}

// --- Keepalive types ---

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

// WriteKeepaliveRequest writes an APF keepalive request with a cookie.
func WriteKeepaliveRequest(w io.Writer, cookie uint32) error {
	buf := [5]byte{APFKeepaliveRequest}
	binary.BigEndian.PutUint32(buf[1:], cookie)
	_, err := w.Write(buf[:])
	return err
}

// WriteKeepaliveReply writes an APF keepalive reply echoing the cookie.
func WriteKeepaliveReply(w io.Writer, cookie uint32) error {
	buf := [5]byte{APFKeepaliveReply}
	binary.BigEndian.PutUint32(buf[1:], cookie)
	_, err := w.Write(buf[:])
	return err
}

// WriteKeepaliveOptionsRequest writes a keepalive options request.
func WriteKeepaliveOptionsRequest(w io.Writer, interval, timeout uint32) error {
	buf := [9]byte{APFKeepaliveOptionsRequest}
	binary.BigEndian.PutUint32(buf[1:], interval)
	binary.BigEndian.PutUint32(buf[5:], timeout)
	_, err := w.Write(buf[:])
	return err
}

// --- GUID and forward data helpers ---

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

// --- internal read helpers ---

func readString(data []byte, offset int) (string, int, error) {
	if offset+4 > len(data) {
		return "", 0, ErrMessageTooShort
	}
	length := int(binary.BigEndian.Uint32(data[offset:]))
	offset += 4
	if offset+length > len(data) {
		return "", 0, ErrMessageTooShort
	}
	return string(data[offset : offset+length]), offset + length, nil
}

func readFixed(r io.Reader, n int) ([]byte, error) {
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func readStringMsg(r io.Reader) ([]byte, error) {
	var lenBuf [4]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return nil, err
	}
	strLen := binary.BigEndian.Uint32(lenBuf[:])
	if strLen > maxAPFStringLen {
		return nil, fmt.Errorf("apf: service name too long: %d", strLen)
	}
	buf := make([]byte, 4+strLen)
	copy(buf, lenBuf[:])
	if _, err := io.ReadFull(r, buf[4:]); err != nil {
		return nil, err
	}
	return buf, nil
}

func readUserAuthRequest(r io.Reader) ([]byte, error) {
	// Read three length-prefixed strings: username, service, method
	result := make([]byte, 0, 3*(4+64))
	for i := 0; i < 3; i++ {
		var lenBuf [4]byte
		if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
			return nil, err
		}
		strLen := binary.BigEndian.Uint32(lenBuf[:])
		if strLen > maxAPFStringLen {
			return nil, fmt.Errorf("apf: auth string too long: %d", strLen)
		}
		strBuf := make([]byte, strLen)
		if _, err := io.ReadFull(r, strBuf); err != nil {
			return nil, err
		}
		result = append(result, lenBuf[:]...)
		result = append(result, strBuf...)
	}
	return result, nil
}

func readGlobalRequest(r io.Reader) ([]byte, error) {
	// request name (string) + want_reply (1 byte) + variable data
	var lenBuf [4]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return nil, err
	}
	strLen := binary.BigEndian.Uint32(lenBuf[:])
	if strLen > maxAPFStringLen {
		return nil, fmt.Errorf("apf: request name too long: %d", strLen)
	}
	// Read name + want_reply byte
	buf := make([]byte, 4+strLen+1)
	copy(buf, lenBuf[:])
	if _, err := io.ReadFull(r, buf[4:]); err != nil {
		return nil, err
	}

	// The remaining data depends on the request type.
	// For tcpip-forward: string(address) + uint32(port)
	// Read remaining available data up to a reasonable limit.
	reqName := string(buf[4 : 4+strLen])
	if reqName == "tcpip-forward" || reqName == "cancel-tcpip-forward" {
		extra, err := readForwardData(r)
		if err != nil {
			return nil, err
		}
		buf = append(buf, extra...)
	}
	return buf, nil
}

func readForwardData(r io.Reader) ([]byte, error) {
	// string(address) + uint32(port)
	var lenBuf [4]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return nil, err
	}
	strLen := binary.BigEndian.Uint32(lenBuf[:])
	if strLen > maxAPFStringLen {
		return nil, fmt.Errorf("apf: forward address too long: %d", strLen)
	}
	buf := make([]byte, 4+strLen+4) // len + string + port
	copy(buf, lenBuf[:])
	if _, err := io.ReadFull(r, buf[4:]); err != nil {
		return nil, err
	}
	return buf, nil
}

func readChannelOpen(r io.Reader) ([]byte, error) {
	// channel type (string) + sender_ch + window + max_pkt + optional data
	var lenBuf [4]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return nil, err
	}
	strLen := binary.BigEndian.Uint32(lenBuf[:])
	if strLen > maxAPFStringLen {
		return nil, fmt.Errorf("apf: channel type too long: %d", strLen)
	}
	// name + 3 uint32s (sender, window, max_pkt) = 12 bytes
	headerSize := 4 + int(strLen) + 12
	buf := make([]byte, headerSize)
	copy(buf, lenBuf[:])
	if _, err := io.ReadFull(r, buf[4:]); err != nil {
		return nil, err
	}

	// Channel open may have additional data (connected address/port/origin)
	// For "direct-tcpip" and "forwarded-tcpip": 2 strings + 2 uint32s
	chType := string(buf[4 : 4+strLen])
	if chType == "direct-tcpip" || chType == "forwarded-tcpip" {
		extra, err := readChannelOpenExtra(r)
		if err != nil {
			return nil, err
		}
		buf = append(buf, extra...)
	}
	return buf, nil
}

func readChannelOpenExtra(r io.Reader) ([]byte, error) {
	// 2 strings (connected addr, origin addr) + 2 uint32s (connected port, origin port)
	var result []byte
	for i := 0; i < 2; i++ {
		var lenBuf [4]byte
		if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
			return nil, err
		}
		strLen := binary.BigEndian.Uint32(lenBuf[:])
		if strLen > maxAPFStringLen {
			return nil, fmt.Errorf("apf: address too long: %d", strLen)
		}
		// string + uint32 port
		seg := make([]byte, 4+strLen+4)
		copy(seg, lenBuf[:])
		if _, err := io.ReadFull(r, seg[4:]); err != nil {
			return nil, err
		}
		result = append(result, seg...)
	}
	return result, nil
}

func readChannelData(r io.Reader) ([]byte, error) {
	// recipient channel (uint32) + data (string)
	var header [8]byte // channel + data length
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	dataLen := binary.BigEndian.Uint32(header[4:])
	if dataLen > 1<<20 { // 1 MiB safety limit
		return nil, fmt.Errorf("apf: channel data too large: %d", dataLen)
	}
	buf := make([]byte, 8+dataLen)
	copy(buf, header[:])
	if _, err := io.ReadFull(r, buf[8:]); err != nil {
		return nil, err
	}
	return buf, nil
}

func readDisconnect(r io.Reader) ([]byte, error) {
	return readFixed(r, 4) // reason code
}

func readProtocolVersion(r io.Reader) ([]byte, error) {
	return readFixed(r, 28) // major + minor + trigger + UUID (16)
}

func writeStringMsg(w io.Writer, msgType uint8, s string) error {
	buf := make([]byte, 1+4+len(s))
	buf[0] = msgType
	binary.BigEndian.PutUint32(buf[1:], uint32(len(s)))
	copy(buf[5:], s)
	_, err := w.Write(buf)
	return err
}
