// Package stunhelper answers STUN Binding requests on the agent QUIC port, telling each asker the
// public address its request came from, so the two sides of a session can try a direct link.
package stunhelper

import (
	"context"
	"encoding/binary"
	"errors"
	"hash/maphash"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/pion/stun/v3"
)

// MaxRequestBytes is RFC 8489's size for a request over a path whose MTU is unknown.
const MaxRequestBytes = 548

// The results the helper reports for each packet it reads.
const (
	ResultAnswered     = "answered"
	ResultMalformed    = "malformed"
	ResultAddressLimit = "address_limit"
	ResultTotalLimit   = "total_limit"
)

// Limits bound what the helper answers in each second.
type Limits struct {
	// PerAddress is the answers one address gets each second, whatever port it asks from.
	PerAddress int
	// Total is the answers the helper gives each second in all.
	Total int
	// Slots is the size of the fixed table the per-address counts live in.
	Slots int
}

// DefaultLimits hold an office of 50 technicians opening sessions in one second behind one
// address, with every ask repeated, and the largest burst the load profiles start.
var DefaultLimits = Limits{PerAddress: 400, Total: 4000, Slots: 4096}

// slot counts one address's answers in one second; a colliding address takes the slot over.
type slot struct {
	addr   netip.Addr
	second int64
	count  int
}

// Helper answers Binding requests within its Limits. Its memory is fixed at construction.
type Helper struct {
	limits Limits
	seed   maphash.Seed

	mu          sync.Mutex
	table       []slot
	totalSecond int64
	totalCount  int
}

// New returns a helper bounded by limits.
func New(limits Limits) *Helper {
	return &Helper{limits: limits, seed: maphash.MakeSeed(), table: make([]slot, limits.Slots)}
}

// Answer returns the reply to one packet from asker at now, or nil when the packet is dropped,
// with the result either way.
func (h *Helper) Answer(packet []byte, asker netip.AddrPort, now time.Time) ([]byte, string) {
	if !isBindingRequest(packet) {
		return nil, ResultMalformed
	}
	if result := h.admit(asker.Addr().Unmap(), now.Unix()); result != ResultAnswered {
		return nil, result
	}
	var request stun.Message
	request.Raw = packet
	if err := request.Decode(); err != nil {
		return nil, ResultMalformed
	}
	reply, err := stun.Build(
		stun.NewTransactionIDSetter(request.TransactionID),
		stun.BindingSuccess,
		&stun.XORMappedAddress{IP: asker.Addr().Unmap().AsSlice(), Port: int(asker.Port())},
	)
	if err != nil {
		return nil, ResultMalformed
	}
	return reply.Raw, ResultAnswered
}

// headerBytes is a STUN header's size; its length field counts what follows it.
const headerBytes = 20

// isBindingRequest reads the header alone: a Binding request, the magic cookie, a length that
// matches the packet, and no more than MaxRequestBytes in all. Attributes are not read.
func isBindingRequest(packet []byte) bool {
	if len(packet) > MaxRequestBytes || !stun.IsMessage(packet) {
		return false
	}
	if binary.BigEndian.Uint16(packet[0:2]) != stun.BindingRequest.Value() {
		return false
	}
	length := int(binary.BigEndian.Uint16(packet[2:4]))
	return length%4 == 0 && headerBytes+length == len(packet)
}

// admit charges one answer to the asker's address and to the total for this second.
func (h *Helper) admit(addr netip.Addr, second int64) string {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.totalSecond != second {
		h.totalSecond, h.totalCount = second, 0
	}
	if h.totalCount >= h.limits.Total {
		return ResultTotalLimit
	}

	key := addr.As16()
	s := &h.table[maphash.Bytes(h.seed, key[:])%uint64(len(h.table))]
	if s.addr != addr || s.second != second {
		*s = slot{addr: addr, second: second}
	}
	if s.count >= h.limits.PerAddress {
		return ResultAddressLimit
	}
	s.count++
	h.totalCount++
	return ResultAnswered
}

// PacketSource is the shared QUIC socket: packets that are not QUIC, and a way to reply.
type PacketSource interface {
	ReadNonQUICPacket(ctx context.Context, b []byte) (int, net.Addr, error)
	WriteTo(b []byte, addr net.Addr) (int, error)
}

// Serve answers packets from src until ctx ends, reporting each packet's result to observe.
// A reply that cannot be written is dropped; the asker asks again after 500 ms (RFC 8489).
func (h *Helper) Serve(ctx context.Context, src PacketSource, observe func(result string)) error {
	buf := make([]byte, MaxRequestBytes+1)
	for {
		n, from, err := src.ReadNonQUICPacket(ctx, buf)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		udp, ok := from.(*net.UDPAddr)
		if !ok {
			continue
		}
		reply, result := h.Answer(buf[:n], udp.AddrPort(), time.Now())
		observe(result)
		if reply != nil {
			_, _ = src.WriteTo(reply, from)
		}
	}
}
