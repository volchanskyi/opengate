// Package transport implements the Intel AMT Management Presence Server: CIRA over TLS, speaking
// APF, which follows the SSH channel semantics of RFC 4254 with Intel extensions.
package transport

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// APF message type constants per Intel AMT CIRA specification.
const (
	APFDisconnect              uint8 = 1
	APFServiceRequest          uint8 = 5
	APFServiceAccept           uint8 = 6
	APFUserAuthRequest         uint8 = 50
	APFUserAuthSuccess         uint8 = 52
	APFUserAuthFailure         uint8 = 51
	APFGlobalRequest           uint8 = 80
	APFRequestSuccess          uint8 = 81
	APFRequestFailure          uint8 = 82
	APFChannelOpen             uint8 = 90
	APFChannelOpenConfirm      uint8 = 91
	APFChannelOpenFailure      uint8 = 92
	APFChannelWindowAdj        uint8 = 93
	APFChannelData             uint8 = 94
	APFChannelClose            uint8 = 97
	APFProtocolVersion         uint8 = 192
	APFKeepaliveRequest        uint8 = 208
	APFKeepaliveReply          uint8 = 209
	APFKeepaliveOptionsRequest uint8 = 210
	APFKeepaliveOptionsReply   uint8 = 211
)

// APF disconnect reason codes.
const (
	APFDisconnectByApp           uint32 = 11
	APFDisconnectProtocolError   uint32 = 2
	APFDisconnectServiceNotAvail uint32 = 7
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

// maxAPFPayload bounds APF channel-data payload length to fit in the uint32 length field.
// 16 MiB is well above the 32 KiB DefaultMaxPacketSize and well below math.MaxUint32.
const maxAPFPayload = 16 * 1024 * 1024

// ErrMessageTooShort is returned when a message is shorter than expected.
var ErrMessageTooShort = errors.New("apf: message too short")

// ErrUnknownMessageType is returned for unrecognised APF message types.
var ErrUnknownMessageType = errors.New("apf: unknown message type")

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

// readAPFString reads a length-prefixed string plus `trailing` fixed bytes and returns the
// whole wire segment; label names the field in the error for a string over maxAPFStringLen.
func readAPFString(r io.Reader, label string, trailing int) ([]byte, error) {
	var lenBuf [4]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return nil, err
	}
	strLen := binary.BigEndian.Uint32(lenBuf[:])
	if strLen > maxAPFStringLen {
		return nil, fmt.Errorf("apf: %s too long: %d", label, strLen)
	}
	buf := make([]byte, 4+int(strLen)+trailing)
	copy(buf, lenBuf[:])
	if _, err := io.ReadFull(r, buf[4:]); err != nil {
		return nil, err
	}
	return buf, nil
}

func readStringMsg(r io.Reader) ([]byte, error) {
	return readAPFString(r, "service name", 0)
}

func readUserAuthRequest(r io.Reader) ([]byte, error) {
	var result []byte
	for range 3 {
		seg, err := readAPFString(r, "auth string", 0)
		if err != nil {
			return nil, err
		}
		result = append(result, seg...)
	}
	return result, nil
}

func readGlobalRequest(r io.Reader) ([]byte, error) {
	buf, err := readAPFString(r, "request name", 1)
	if err != nil {
		return nil, err
	}
	// Forward requests carry string(address) + uint32(port) after the name.
	strLen := binary.BigEndian.Uint32(buf[:4])
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
	return readAPFString(r, "forward address", 4)
}

func readChannelOpen(r io.Reader) ([]byte, error) {
	// Trailing 12 bytes: sender channel, window and max packet size.
	buf, err := readAPFString(r, "channel type", 12)
	if err != nil {
		return nil, err
	}
	// The direct-tcpip and forwarded-tcpip types carry 2 strings and 2 uint32s.
	strLen := binary.BigEndian.Uint32(buf[:4])
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
	var result []byte
	for range 2 {
		seg, err := readAPFString(r, "address", 4)
		if err != nil {
			return nil, err
		}
		result = append(result, seg...)
	}
	return result, nil
}

func readChannelData(r io.Reader) ([]byte, error) {
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
