package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/vmihailenco/msgpack/v5"
)

var (
	// ErrFrameTooLarge indicates a frame exceeds MaxFrameSize.
	ErrFrameTooLarge = errors.New("frame too large")
	// ErrUnknownFrameType indicates an unrecognized frame type byte.
	ErrUnknownFrameType = errors.New("unknown frame type")
	// ErrIncompleteFrame indicates insufficient data to decode a frame.
	ErrIncompleteFrame = errors.New("incomplete frame")
)

// Codec handles encoding and decoding of protocol frames.
type Codec struct{}

// EncodeControl encodes a ControlMessage to msgpack bytes.
func (c *Codec) EncodeControl(msg *ControlMessage) ([]byte, error) {
	return msgpack.Marshal(msg)
}

// DecodeControl decodes a ControlMessage from msgpack bytes.
func (c *Codec) DecodeControl(data []byte) (*ControlMessage, error) {
	var msg ControlMessage
	if err := msgpack.Unmarshal(data, &msg); err != nil {
		return nil, fmt.Errorf("decode control: %w", err)
	}
	return &msg, nil
}

// EncodeDesktopFrame encodes a DesktopFrame to msgpack bytes.
func (c *Codec) EncodeDesktopFrame(f *DesktopFrame) ([]byte, error) {
	return msgpack.Marshal(f)
}

// DecodeDesktopFrame decodes a DesktopFrame from msgpack bytes.
func (c *Codec) DecodeDesktopFrame(data []byte) (*DesktopFrame, error) {
	var f DesktopFrame
	if err := msgpack.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("decode desktop frame: %w", err)
	}
	return &f, nil
}

// EncodeTerminalFrame encodes a TerminalFrame to msgpack bytes.
func (c *Codec) EncodeTerminalFrame(f *TerminalFrame) ([]byte, error) {
	return msgpack.Marshal(f)
}

// DecodeTerminalFrame decodes a TerminalFrame from msgpack bytes.
func (c *Codec) DecodeTerminalFrame(data []byte) (*TerminalFrame, error) {
	var f TerminalFrame
	if err := msgpack.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("decode terminal frame: %w", err)
	}
	return &f, nil
}

// EncodeFileFrame encodes a FileFrame to msgpack bytes.
func (c *Codec) EncodeFileFrame(f *FileFrame) ([]byte, error) {
	return msgpack.Marshal(f)
}

// DecodeFileFrame decodes a FileFrame from msgpack bytes.
func (c *Codec) DecodeFileFrame(data []byte) (*FileFrame, error) {
	var f FileFrame
	if err := msgpack.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("decode file frame: %w", err)
	}
	return &f, nil
}

// WriteFrame writes a typed frame to a writer.
// Wire format: [type_byte][4-byte big-endian length][payload]
// Ping/Pong are written as a single type byte with no length or payload.
func (c *Codec) WriteFrame(w io.Writer, frameType byte, payload []byte) error {
	if frameType == FramePing || frameType == FramePong {
		_, err := w.Write([]byte{frameType})
		return err
	}

	if len(payload) > MaxFrameSize {
		return fmt.Errorf("%w: %d bytes (max %d)", ErrFrameTooLarge, len(payload), MaxFrameSize)
	}

	header := [5]byte{frameType}
	binary.BigEndian.PutUint32(header[1:], uint32(len(payload)))
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	if len(payload) > 0 {
		_, err := w.Write(payload)
		return err
	}
	return nil
}

// ReadFrame reads a typed frame from a reader.
// Returns the frame type, payload, and any error.
func (c *Codec) ReadFrame(r io.Reader) (byte, []byte, error) {
	var typeBuf [1]byte
	if _, err := io.ReadFull(r, typeBuf[:]); err != nil {
		return 0, nil, err
	}
	frameType := typeBuf[0]

	if frameType == FramePing || frameType == FramePong {
		return frameType, nil, nil
	}

	if frameType != FrameControl && frameType != FrameDesktop &&
		frameType != FrameTerminal && frameType != FrameFile {
		return 0, nil, fmt.Errorf("%w: 0x%02x", ErrUnknownFrameType, frameType)
	}

	var lengthBuf [4]byte
	if _, err := io.ReadFull(r, lengthBuf[:]); err != nil {
		return 0, nil, fmt.Errorf("%w: %v", ErrIncompleteFrame, err)
	}
	length := binary.BigEndian.Uint32(lengthBuf[:])

	if int(length) > MaxFrameSize {
		return 0, nil, fmt.Errorf("%w: %d bytes (max %d)", ErrFrameTooLarge, length, MaxFrameSize)
	}

	payload := make([]byte, length)
	if length > 0 {
		if _, err := io.ReadFull(r, payload); err != nil {
			return 0, nil, fmt.Errorf("%w: %v", ErrIncompleteFrame, err)
		}
	}
	return frameType, payload, nil
}

// EncodeHandshake encodes a handshake message to binary format.
// Wire format matches the Rust HandshakeMessage::encode_binary.
func EncodeHandshake(msgType byte, payload []byte) []byte {
	buf := make([]byte, 1+len(payload))
	buf[0] = msgType
	copy(buf[1:], payload)
	return buf
}

// EncodeServerHello encodes a ServerHello handshake message.
func EncodeServerHello(nonce [32]byte, certHash [48]byte) []byte {
	buf := make([]byte, 81)
	buf[0] = MsgServerHello
	copy(buf[1:33], nonce[:])
	copy(buf[33:81], certHash[:])
	return buf
}

// EncodeAgentHello encodes an AgentHello handshake message.
func EncodeAgentHello(nonce [32]byte, agentCertHash [48]byte) []byte {
	buf := make([]byte, 81)
	buf[0] = MsgAgentHello
	copy(buf[1:33], nonce[:])
	copy(buf[33:81], agentCertHash[:])
	return buf
}

// DecodeHandshakeType returns the type byte from a handshake message.
func DecodeHandshakeType(data []byte) (byte, error) {
	if len(data) == 0 {
		return 0, fmt.Errorf("empty handshake data")
	}
	return data[0], nil
}

// DecodeServerHello decodes a ServerHello handshake message.
func DecodeServerHello(data []byte) (nonce [32]byte, certHash [48]byte, err error) {
	if len(data) != 81 {
		return nonce, certHash, fmt.Errorf("ServerHello must be 81 bytes, got %d", len(data))
	}
	if data[0] != MsgServerHello {
		return nonce, certHash, fmt.Errorf("expected ServerHello type 0x10, got 0x%02x", data[0])
	}
	copy(nonce[:], data[1:33])
	copy(certHash[:], data[33:81])
	return nonce, certHash, nil
}
