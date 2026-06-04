package transport

import (
	"encoding/binary"
	"fmt"
	"io"
)

// This file holds the APF message writers. The wire framing and body readers
// live in apf.go; the typed message structs and parsers in apf_messages.go.

// writeUint32Msg writes an APF message consisting of a single type byte
// followed by the given big-endian uint32 fields. It backs every fixed-layout
// APF writer (channel/keepalive/disconnect) below.
func writeUint32Msg(w io.Writer, msgType uint8, fields ...uint32) error {
	buf := make([]byte, 1+4*len(fields))
	buf[0] = msgType
	for i, v := range fields {
		binary.BigEndian.PutUint32(buf[1+4*i:], v)
	}
	_, err := w.Write(buf)
	return err
}

func writeStringMsg(w io.Writer, msgType uint8, s string) error {
	if len(s) > maxAPFStringLen {
		return fmt.Errorf("apf: string too long: %d bytes (max %d)", len(s), maxAPFStringLen)
	}
	buf := make([]byte, 1+4+len(s))
	buf[0] = msgType
	binary.BigEndian.PutUint32(buf[1:], uint32(len(s))) // #nosec G115 -- bounded above by maxAPFStringLen.
	copy(buf[5:], s)
	_, err := w.Write(buf)
	return err
}

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
	return writeUint32Msg(w, APFChannelOpenConfirm, recipientCh, senderCh, windowSz, maxPacket)
}

// WriteChannelData writes channel data.
func WriteChannelData(w io.Writer, recipientCh uint32, data []byte) error {
	if len(data) > maxAPFPayload {
		return fmt.Errorf("apf: channel data too large: %d bytes (max %d)", len(data), maxAPFPayload)
	}
	buf := make([]byte, 9+len(data))
	buf[0] = APFChannelData
	binary.BigEndian.PutUint32(buf[1:], recipientCh)
	binary.BigEndian.PutUint32(buf[5:], uint32(len(data))) // #nosec G115 -- bounded above by maxAPFPayload.
	copy(buf[9:], data)
	_, err := w.Write(buf)
	return err
}

// WriteChannelClose writes a channel close message.
func WriteChannelClose(w io.Writer, recipientCh uint32) error {
	return writeUint32Msg(w, APFChannelClose, recipientCh)
}

// WriteChannelWindowAdj writes a window adjust message.
func WriteChannelWindowAdj(w io.Writer, recipientCh, bytesToAdd uint32) error {
	return writeUint32Msg(w, APFChannelWindowAdj, recipientCh, bytesToAdd)
}

// WriteDisconnect writes an APF disconnect message.
func WriteDisconnect(w io.Writer, reasonCode uint32) error {
	return writeUint32Msg(w, APFDisconnect, reasonCode)
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

// WriteKeepaliveRequest writes an APF keepalive request with a cookie.
func WriteKeepaliveRequest(w io.Writer, cookie uint32) error {
	return writeUint32Msg(w, APFKeepaliveRequest, cookie)
}

// WriteKeepaliveReply writes an APF keepalive reply echoing the cookie.
func WriteKeepaliveReply(w io.Writer, cookie uint32) error {
	return writeUint32Msg(w, APFKeepaliveReply, cookie)
}

// WriteKeepaliveOptionsRequest writes a keepalive options request.
func WriteKeepaliveOptionsRequest(w io.Writer, interval, timeout uint32) error {
	return writeUint32Msg(w, APFKeepaliveOptionsRequest, interval, timeout)
}
