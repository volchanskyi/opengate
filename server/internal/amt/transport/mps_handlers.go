package transport

import (
	"encoding/binary"
	"fmt"
	"io"
)

// handleMessage dispatches a post-handshake APF message.
func (s *Server) handleMessage(mc *Conn, msgType uint8, payload []byte) error {
	switch msgType {
	case APFGlobalRequest:
		gr, err := ParseGlobalRequest(payload)
		if err != nil {
			return err
		}
		mc.logger.Debug("global request", "request", gr.RequestName)
		recordBoundPort(mc, &gr)
		if gr.WantReply {
			return WriteRequestSuccess(mc.netConn)
		}
		return nil

	case APFChannelOpen:
		return s.handleChannelOpen(mc, payload)

	case APFChannelOpenConfirm:
		return mc.confirmOpen(payload)

	case APFChannelOpenFailure:
		return mc.refuseOpen(payload)

	case APFChannelData:
		return s.handleChannelData(mc, payload)

	case APFChannelClose:
		if len(payload) < 4 {
			return ErrMessageTooShort
		}
		ch := binary.BigEndian.Uint32(payload)
		return s.handleChannelClose(mc, ch)

	case APFChannelWindowAdj:
		return s.handleWindowAdj(mc, payload)

	case APFKeepaliveRequest:
		ka, err := ParseKeepaliveRequest(payload)
		if err != nil {
			return err
		}
		return WriteKeepaliveReply(mc.netConn, ka.Cookie)

	case APFKeepaliveReply:
		return nil // Keepalive acknowledged.

	case APFKeepaliveOptionsReply:
		return nil // Logged at debug; server controls the schedule.

	case APFDisconnect:
		return io.EOF // Clean disconnect.

	default:
		mc.logger.Warn("unhandled APF message", "type", msgType)
		return nil
	}
}

// handleWindowAdj processes a window adjust message, increasing the channel's send credits.
func (s *Server) handleWindowAdj(mc *Conn, payload []byte) error {
	if len(payload) < 8 {
		return ErrMessageTooShort
	}
	recipientCh := binary.BigEndian.Uint32(payload[0:4])
	bytesToAdd := binary.BigEndian.Uint32(payload[4:8])
	if ch, ok := mc.channel(recipientCh); ok {
		ch.mu.Lock()
		ch.sendWindow += int64(bytesToAdd)
		ch.mu.Unlock()
	}
	return nil
}

// handleChannelOpen processes an APF channel open request from the AMT device.
func (s *Server) handleChannelOpen(mc *Conn, payload []byte) error {
	co, err := ParseChannelOpen(payload)
	if err != nil {
		return err
	}

	mc.mu.Lock()
	localCh := mc.nextChanID
	mc.nextChanID++
	ch := newChannel(mc, localCh, co.SenderChannel, co.ChannelType, co.InitialWindowSz)
	ch.discard = true
	mc.channels[localCh] = ch
	mc.mu.Unlock()

	mc.logger.Info("channel opened",
		"type", co.ChannelType,
		"local_ch", localCh,
		"remote_ch", co.SenderChannel)

	return WriteChannelOpenConfirm(mc.netConn,
		co.SenderChannel, localCh,
		DefaultWindowSize, DefaultMaxPacketSize)
}

// handleChannelData hands data received on an APF channel to that channel.
func (s *Server) handleChannelData(mc *Conn, payload []byte) error {
	cd, err := ParseChannelData(payload)
	if err != nil {
		return err
	}

	ch, ok := mc.channel(cd.RecipientChannel)
	if !ok {
		mc.logger.Warn("data for unknown channel", "channel", cd.RecipientChannel)
		return nil
	}

	adjust, err := ch.deliver(cd.Data)
	if err != nil {
		mc.logger.Warn("closing channel", "channel", cd.RecipientChannel, "error", err)
		ch.end(err)
		mc.forget(ch.LocalID)
		return WriteChannelClose(mc.netConn, ch.RemoteID)
	}
	if adjust > 0 {
		if err := WriteChannelWindowAdj(mc.netConn, ch.RemoteID, adjust); err != nil {
			return fmt.Errorf("write window adjust: %w", err)
		}
	}
	return nil
}

// handleChannelClose ends a channel the device closed and answers with a close of its own.
func (s *Server) handleChannelClose(mc *Conn, localCh uint32) error {
	ch, ok := mc.channel(localCh)
	if !ok {
		return nil
	}
	mc.forget(localCh)
	ch.end(io.EOF)

	mc.logger.Info("channel closed", "local_ch", localCh)
	return WriteChannelClose(mc.netConn, ch.RemoteID)
}
