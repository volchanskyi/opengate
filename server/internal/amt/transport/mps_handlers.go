package transport

import (
	"encoding/binary"
	"fmt"
	"io"
)

// This file holds post-handshake APF message dispatch and per-message handlers.
// The server core lives in mps.go; the handshake in mps_handshake.go; the
// Conn/Channel types in mps_conn.go.

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
	mc.mu.RLock()
	ch, ok := mc.channels[recipientCh]
	mc.mu.RUnlock()
	if ok {
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
	ch := &Channel{
		LocalID:    localCh,
		RemoteID:   co.SenderChannel,
		Type:       co.ChannelType,
		sendWindow: int64(co.InitialWindowSz),
	}
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

// handleChannelData processes data received on an APF channel.
func (s *Server) handleChannelData(mc *Conn, payload []byte) error {
	cd, err := ParseChannelData(payload)
	if err != nil {
		return err
	}

	mc.mu.RLock()
	ch, ok := mc.channels[cd.RecipientChannel]
	mc.mu.RUnlock()

	if !ok {
		mc.logger.Warn("data for unknown channel", "channel", cd.RecipientChannel)
		return nil
	}

	// Forward data via callback or TCP connection.
	ch.mu.Lock()
	fwd := ch.fwd
	onData := ch.OnData
	ch.recvConsumed += int64(len(cd.Data))
	consumed := ch.recvConsumed
	ch.mu.Unlock()

	if onData != nil {
		onData(cd.Data)
	} else if fwd != nil {
		if _, err := fwd.Write(cd.Data); err != nil {
			mc.logger.Error("forward write error", "channel", cd.RecipientChannel, "error", err)
			return s.handleChannelClose(mc, cd.RecipientChannel)
		}
	}

	// Send WindowAdj when consumed exceeds half of our advertised window.
	if consumed >= int64(DefaultWindowSize)/2 {
		ch.mu.Lock()
		ch.recvConsumed = 0
		ch.mu.Unlock()
		// #nosec G115 -- consumed is bounded above by DefaultWindowSize (uint32).
		if err := WriteChannelWindowAdj(mc.netConn, ch.RemoteID, uint32(consumed)); err != nil {
			return fmt.Errorf("write window adjust: %w", err)
		}
	}

	return nil
}

// handleChannelClose closes a channel and its forwarding connection.
func (s *Server) handleChannelClose(mc *Conn, localCh uint32) error {
	mc.mu.Lock()
	ch, ok := mc.channels[localCh]
	if ok {
		delete(mc.channels, localCh)
	}
	mc.mu.Unlock()

	if !ok {
		return nil
	}

	ch.mu.Lock()
	if ch.fwd != nil {
		_ = ch.fwd.Close()
	}
	ch.mu.Unlock()

	mc.logger.Info("channel closed", "local_ch", localCh)
	return WriteChannelClose(mc.netConn, ch.RemoteID)
}
