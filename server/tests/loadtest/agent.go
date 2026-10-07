package main

import (
	"context"
	"crypto/rand"
	"crypto/sha512"
	"crypto/tls"
	"fmt"
	"io"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

func runAgent(credentials agentCredentials, addr string, plan tenantAgent, opts loadOptions,
	presence fleetPresence,
) agentResult {
	// The deadline spans connecting, registering and the requested hold, so a held-open fleet
	// is not cut short by a fixed budget.
	ctx, cancel := context.WithTimeout(context.Background(), agentDeadline+opts.holdFor)
	defer cancel()
	return runAgentWithContext(ctx, credentials, addr, plan, opts, presence)
}

// runAgentWithContext runs one machine's whole life, bounded by the caller's context.
// The credential is obtained once and reused on every reconnect, so the server sees one machine.
func runAgentWithContext(ctx context.Context, credentials agentCredentials, addr string,
	plan tenantAgent, opts loadOptions, presence fleetPresence,
) agentResult {
	tlsConfig, err := credentials.forAgent(ctx, plan)
	if err != nil {
		return agentResult{err: err}
	}

	// The stay is measured from here, so time spent behind a dark link counts against it.
	leaveAt := time.Now().Add(opts.holdFor)

	return persistThrough(ctx, opts, func(ctx context.Context) agentResult {
		thisConnection := opts
		thisConnection.holdFor = time.Until(leaveAt)
		res := serveOneConnection(ctx, addr, tlsConfig, plan, thisConnection, presence.Arrived)
		// A connection that never registered was never counted as attached.
		if !res.arrivedAt.IsZero() && presence.Left != nil {
			presence.Left()
		}
		return res
	})
}

// serveOneConnection runs one machine on one connection until the connection breaks or the run
// ends.
func serveOneConnection(ctx context.Context, addr string, tlsConfig *tls.Config,
	plan tenantAgent, opts loadOptions, noteArrival func(),
) agentResult {
	t0 := time.Now()
	conn, err := quic.DialAddr(ctx, addr, tlsConfig, &quic.Config{
		MaxIdleTimeout: 30 * time.Second,
	})
	if err != nil {
		return agentResult{err: fmt.Errorf("dial: %w", err)}
	}
	res := agentResult{connectDur: time.Since(t0)}
	defer conn.CloseWithError(0, "loadtest done")

	// The agent opens the control stream and writes first, per RFC 9000 stream discovery.
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
	// The arrival is announced once registered, because the machine outlives the phase it arrived in.
	res.arrivedAt = time.Now()
	if noteArrival != nil {
		noteArrival()
	}

	if err := runSoakTraffic(ctx, codec, stream, opts); err != nil {
		res.err = err
		return res
	}

	if err := proveUntilWoundDown(ctx, codec, stream, opts); err != nil {
		res.err = err
	}
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

// agentCapabilities advertises Terminal always, plus Backfill when backfill batches are
// configured, because the server gates backfill admission on the advertised capability.
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
