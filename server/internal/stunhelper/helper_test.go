package stunhelper

import (
	"encoding/binary"
	"net/netip"
	"testing"
	"time"

	"github.com/pion/stun/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bindingRequest is a well-formed Binding request carrying extra bytes of attributes.
func bindingRequest(t *testing.T, padding int) ([]byte, stun.Message) {
	t.Helper()
	setters := []stun.Setter{stun.TransactionID, stun.BindingRequest}
	if padding > 0 {
		setters = append(setters, stun.NewSoftware(string(make([]byte, padding))))
	}
	msg, err := stun.Build(setters...)
	require.NoError(t, err)
	return append([]byte(nil), msg.Raw...), *msg
}

var asker = netip.MustParseAddrPort("203.0.113.7:51234")

func TestAWellFormedBindingRequestIsAnsweredWithTheAskersAddress(t *testing.T) {
	h := New(Limits{PerAddress: 10, Total: 100, Slots: 16})
	request, sent := bindingRequest(t, 0)

	reply, result := h.Answer(request, asker, time.Unix(1000, 0))

	require.Equal(t, ResultAnswered, result)
	var got stun.Message
	got.Raw = reply
	require.NoError(t, got.Decode())
	assert.Equal(t, stun.BindingSuccess, got.Type)
	assert.Equal(t, sent.TransactionID, got.TransactionID, "the reply answers this very request")

	var mapped stun.XORMappedAddress
	require.NoError(t, mapped.GetFrom(&got))
	assert.Equal(t, asker.Addr().AsSlice(), []byte(mapped.IP.To4()))
	assert.Equal(t, int(asker.Port()), mapped.Port)

	assert.Len(t, reply, 32, "the reply carries the address and nothing else")
	assert.Less(t, len(reply), 2*len(request), "an answer is under twice its request")
}

func TestAnIPv6AskerIsAnsweredWithItsAddress(t *testing.T) {
	h := New(Limits{PerAddress: 10, Total: 100, Slots: 16})
	request, _ := bindingRequest(t, 0)
	v6 := netip.MustParseAddrPort("[2001:db8::7]:4000")

	reply, result := h.Answer(request, v6, time.Unix(1000, 0))

	require.Equal(t, ResultAnswered, result)
	var got stun.Message
	got.Raw = reply
	require.NoError(t, got.Decode())
	var mapped stun.XORMappedAddress
	require.NoError(t, mapped.GetFrom(&got))
	assert.Equal(t, v6.Addr().AsSlice(), []byte(mapped.IP))
}

func TestARequestThatIsNotAWellFormedBindingRequestIsDropped(t *testing.T) {
	h := New(Limits{PerAddress: 10, Total: 100, Slots: 16})
	at := time.Unix(1000, 0)

	request, _ := bindingRequest(t, 0)
	oversized, _ := bindingRequest(t, MaxRequestBytes)
	indication := append([]byte(nil), request...)
	binary.BigEndian.PutUint16(indication[0:2], uint16(stun.MessageType{
		Method: stun.MethodBinding, Class: stun.ClassIndication,
	}.Value()))
	wrongCookie := append([]byte(nil), request...)
	binary.BigEndian.PutUint32(wrongCookie[4:8], 0xdeadbeef)
	lengthLies := append([]byte(nil), request...)
	binary.BigEndian.PutUint16(lengthLies[2:4], 8)

	for name, packet := range map[string][]byte{
		"a request over 548 bytes":    oversized,
		"a binding indication":        indication,
		"a wrong magic cookie":        wrongCookie,
		"a length past the packet":    lengthLies,
		"a header cut short":          request[:12],
		"a QUIC packet's first bytes": {0xc0, 0x00, 0x00, 0x00, 0x01},
	} {
		reply, result := h.Answer(packet, asker, at)
		assert.Nilf(t, reply, "%s is not answered", name)
		assert.Equalf(t, ResultMalformed, result, "%s", name)
	}
	assert.Len(t, oversized, len(request)+4+MaxRequestBytes, "the oversized probe is past the limit")
}

func TestOneAddressIsAnsweredUpToItsAllowanceEachSecond(t *testing.T) {
	h := New(Limits{PerAddress: 3, Total: 100, Slots: 16})
	request, _ := bindingRequest(t, 0)
	second := time.Unix(1000, 0)

	for range 3 {
		_, result := h.Answer(request, asker, second.Add(100*time.Millisecond))
		require.Equal(t, ResultAnswered, result)
	}
	_, result := h.Answer(request, asker, second.Add(900*time.Millisecond))
	assert.Equal(t, ResultAddressLimit, result, "past its allowance an address is dropped")

	sameHostOtherPort := netip.AddrPortFrom(asker.Addr(), 9999)
	_, result = h.Answer(request, sameHostOtherPort, second.Add(900*time.Millisecond))
	assert.Equal(t, ResultAddressLimit, result, "an address is the host, whatever port it asks from")

	// Another address can share the asker's slot under the per-run seed and reset it, so it asks last.
	other := netip.MustParseAddrPort("198.51.100.9:1000")
	_, result = h.Answer(request, other, second.Add(950*time.Millisecond))
	assert.Equal(t, ResultAnswered, result, "one address flooding uses only its own allowance")

	_, result = h.Answer(request, asker, second.Add(time.Second))
	assert.Equal(t, ResultAnswered, result, "the next second brings a fresh allowance")
}

func TestTheHelperAnswersNoMoreThanItsTotalEachSecond(t *testing.T) {
	h := New(Limits{PerAddress: 10, Total: 5, Slots: 16})
	request, _ := bindingRequest(t, 0)
	second := time.Unix(1000, 0)

	answered := 0
	for i := range 20 {
		from := netip.AddrPortFrom(netip.AddrFrom4([4]byte{10, 0, 0, byte(i)}), 5000)
		if _, result := h.Answer(request, from, second); result == ResultAnswered {
			answered++
		} else {
			assert.Equal(t, ResultTotalLimit, result)
		}
	}
	assert.Equal(t, 5, answered)

	_, result := h.Answer(request, asker, second.Add(time.Second))
	assert.Equal(t, ResultAnswered, result)
}

func TestAFloodFromForgedAddressesLeavesTheTableItsSize(t *testing.T) {
	h := New(Limits{PerAddress: 2, Total: 1_000_000, Slots: 64})
	request, _ := bindingRequest(t, 0)
	second := time.Unix(1000, 0)

	for i := range 100_000 {
		from := netip.AddrPortFrom(netip.AddrFrom4([4]byte{10, byte(i >> 16), byte(i >> 8), byte(i)}), 1)
		h.Answer(request, from, second)
	}

	assert.Len(t, h.table, 64, "the per-address counts never grow past their fixed table")
	assert.Equal(t, 64, cap(h.table))
}

func TestDefaultLimitsHoldAnOfficeAndTheLargestLoadBurst(t *testing.T) {
	// An office of 50 technicians behind one address opens sessions in one second, each browser
	// asking from up to three interfaces and asking again once 500 ms later.
	const officeBurst = 50 * 3 * 2
	assert.GreaterOrEqual(t, DefaultLimits.PerAddress, officeBurst)
	assert.Greater(t, DefaultLimits.Total, DefaultLimits.PerAddress)
	assert.Positive(t, DefaultLimits.Slots)
}
