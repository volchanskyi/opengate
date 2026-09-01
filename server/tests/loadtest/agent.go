package main

import (
	"context"
	"crypto/rand"
	"crypto/sha512"
	"fmt"
	"io"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// One machine's life on the wire: dial, handshake, register, behave, and stay
// until the run says otherwise.

func runAgent(credentials agentCredentials, addr string, plan tenantAgent, opts loadOptions) agentResult {
	// The deadline covers connecting and registering, plus however long this
	// machine was asked to stay. A fixed budget would cut a held-open fleet
	// short and report the run's own timeout as the server dropping machines.
	ctx, cancel := context.WithTimeout(context.Background(), agentDeadline+opts.holdFor)
	defer cancel()
	return runAgentWithContext(ctx, credentials, addr, plan, opts)
}

// runAgentWithContext is one machine's whole life, bounded by the caller's
// context rather than by a budget of its own. A fleet walking a profile decides
// when each machine leaves, and a machine that closed because the run wound it
// down is not one the server dropped.
func runAgentWithContext(ctx context.Context, credentials agentCredentials, addr string,
	plan tenantAgent, opts loadOptions,
) agentResult {
	tlsConfig, err := credentials.forAgent(ctx, plan)
	if err != nil {
		return agentResult{err: err}
	}

	// Connect.
	t0 := time.Now()
	conn, err := quic.DialAddr(ctx, addr, tlsConfig, &quic.Config{
		MaxIdleTimeout: 30 * time.Second,
	})
	if err != nil {
		return agentResult{err: fmt.Errorf("dial: %w", err)}
	}
	res := agentResult{connectDur: time.Since(t0)}
	defer conn.CloseWithError(0, "loadtest done")

	// Open control stream (agent-initiated): the agent opens and writes first,
	// per RFC 9000 stream-discovery.
	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		res.err = fmt.Errorf("open stream: %w", err)
		return res
	}

	t1 := time.Now()
	if err := handshake(stream, tlsConfig.Certificates[0].Certificate[0]); err != nil {
		res.err = err
		return res
	}
	res.handshakeDur = time.Since(t1)

	codec := &protocol.Codec{}
	t2 := time.Now()
	if err := register(codec, stream, plan.hostname, agentCapabilities(opts)); err != nil {
		res.err = err
		return res
	}
	res.registerDur = time.Since(t2)
	// The machine is part of the fleet from here. What follows — its traffic and
	// whatever hold the run asked for — is the fleet being carried, not the
	// fleet arriving.
	res.arrivedAt = time.Now()

	if err := runSoakTraffic(codec, stream, opts); err != nil {
		res.err = err
		return res
	}

	// Whatever hold the traffic asked for has been served; the machine now stays
	// until the run says otherwise. A fleet holding a level is what an estate
	// actually looks like, and it is the load a server spends most of its time
	// carrying.
	<-ctx.Done()
	return res
}

// handshake performs the agent-first mTLS control handshake: it sends AgentHello
// (nonce + cert hash) and reads the fixed-size ServerHello reply.
func handshake(stream io.ReadWriter, certDER []byte) error {
	certHash := sha512.Sum384(certDER)
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return fmt.Errorf("generate nonce: %w", err)
	}
	if _, err := stream.Write(protocol.EncodeAgentHello(nonce, certHash)); err != nil {
		return fmt.Errorf("write agent hello: %w", err)
	}
	if _, err := io.ReadFull(stream, make([]byte, 81)); err != nil {
		return fmt.Errorf("read server hello: %w", err)
	}
	return nil
}

// agentCapabilities advertises the capabilities the soak exercises: Terminal
// always, plus Backfill when the reconnect-storm scenario is enabled (the
// server gates backfill admission on the advertised capability).
func agentCapabilities(opts loadOptions) []protocol.AgentCapability {
	caps := []protocol.AgentCapability{protocol.CapTerminal}
	if opts.backfillBatches > 0 {
		caps = append(caps, protocol.CapBackfill)
	}
	return caps
}

// register sends the AgentRegister control frame that completes enrollment.
func register(codec *protocol.Codec, w io.Writer, hostname string, caps []protocol.AgentCapability) error {
	payload, err := codec.EncodeControl(&protocol.ControlMessage{
		Type:         protocol.MsgAgentRegister,
		Capabilities: caps,
		Hostname:     hostname,
		OS:           "linux",
		Arch:         "amd64",
		Version:      "0.1.0",
	})
	if err != nil {
		return fmt.Errorf("encode register: %w", err)
	}
	if err := codec.WriteFrame(w, protocol.FrameControl, payload); err != nil {
		return fmt.Errorf("write register: %w", err)
	}
	return nil
}
