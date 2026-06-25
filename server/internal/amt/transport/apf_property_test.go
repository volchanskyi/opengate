package transport

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// Property-based coverage for the APF byte-level parsers, complementing the
// boundary table tests in apf_boundary_test.go. Two classes of invariant:
//   - round-trip: a value written by the writer is recovered intact by
//     ReadMessage + the matching Parse* function;
//   - robustness: every Parse* / ReadMessage tolerates arbitrary bytes without
//     panicking (a typed error is the only acceptable failure).
//
// rapid.Check always runs under `go test` (no skip/build-tag) and explores a
// bounded number of cases deterministically, per tests-determinism.md.

// drawBytes draws a byte slice of length in [0, max].
func drawBytes(t *rapid.T, label string, max int) []byte {
	return rapid.SliceOfN(rapid.Byte(), 0, max).Draw(t, label)
}

// roundTripPayload writes one APF message, reads it back through ReadMessage,
// asserts the framed message type, and returns the payload for the caller's
// Parse* assertion. It collapses the write→read→type-check skeleton that every
// round-trip property below would otherwise repeat verbatim.
func roundTripPayload(t *rapid.T, write func(io.Writer) error, want uint8) []byte {
	var buf bytes.Buffer
	require.NoError(t, write(&buf))
	mt, payload, err := ReadMessage(&buf)
	require.NoError(t, err)
	require.Equal(t, want, mt)
	return payload
}

// TestProperty_ServiceRequest_RoundTrip asserts a service name survives
// WriteServiceAccept → ReadMessage → ParseServiceRequest unchanged.
func TestProperty_ServiceRequest_RoundTrip(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		name := string(drawBytes(t, "name", maxAPFStringLen))

		payload := roundTripPayload(t, func(w io.Writer) error {
			return WriteServiceAccept(w, name)
		}, APFServiceAccept)

		sr, err := ParseServiceRequest(payload)
		require.NoError(t, err)
		require.Equal(t, name, sr.ServiceName)
	})
}

// TestProperty_ChannelData_RoundTrip asserts (channel, data) survives
// WriteChannelData → ReadMessage → ParseChannelData. Data is bounded well under
// the 1 MiB read cap.
func TestProperty_ChannelData_RoundTrip(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		ch := rapid.Uint32().Draw(t, "ch")
		data := drawBytes(t, "data", 8192)

		payload := roundTripPayload(t, func(w io.Writer) error {
			return WriteChannelData(w, ch, data)
		}, APFChannelData)

		cd, err := ParseChannelData(payload)
		require.NoError(t, err)
		require.Equal(t, ch, cd.RecipientChannel)
		require.Equal(t, data, cd.Data)
	})
}

// TestProperty_ProtocolVersion_RoundTrip asserts the version triple survives
// WriteProtocolVersion → ReadMessage → ParseProtocolVersion. The writer emits a
// zero UUID, so the parsed UUID must be all-zero.
func TestProperty_ProtocolVersion_RoundTrip(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		major := rapid.Uint32().Draw(t, "major")
		minor := rapid.Uint32().Draw(t, "minor")
		trigger := rapid.Uint32().Draw(t, "trigger")

		payload := roundTripPayload(t, func(w io.Writer) error {
			return WriteProtocolVersion(w, major, minor, trigger)
		}, APFProtocolVersion)

		pv, err := ParseProtocolVersion(payload)
		require.NoError(t, err)
		require.Equal(t, major, pv.MajorVersion)
		require.Equal(t, minor, pv.MinorVersion)
		require.Equal(t, trigger, pv.Trigger)
		require.Equal(t, [16]byte{}, pv.UUID)
	})
}

// TestProperty_Keepalive_RoundTrip asserts the cookie and the interval/timeout
// pair survive their writers + parsers.
func TestProperty_Keepalive_RoundTrip(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		cookie := rapid.Uint32().Draw(t, "cookie")
		kpayload := roundTripPayload(t, func(w io.Writer) error {
			return WriteKeepaliveRequest(w, cookie)
		}, APFKeepaliveRequest)
		kr, err := ParseKeepaliveRequest(kpayload)
		require.NoError(t, err)
		require.Equal(t, cookie, kr.Cookie)

		interval := rapid.Uint32().Draw(t, "interval")
		timeout := rapid.Uint32().Draw(t, "timeout")
		opayload := roundTripPayload(t, func(w io.Writer) error {
			return WriteKeepaliveOptionsRequest(w, interval, timeout)
		}, APFKeepaliveOptionsRequest)
		ko, err := ParseKeepaliveOptions(opayload)
		require.NoError(t, err)
		require.Equal(t, interval, ko.Interval)
		require.Equal(t, timeout, ko.Timeout)
	})
}

// TestProperty_Parsers_NeverPanic feeds arbitrary bytes to every APF parser and
// to ReadMessage (whose first byte selects an arbitrary message type). A typed
// error is fine; a panic (e.g. an unchecked slice index) fails the test.
func TestProperty_Parsers_NeverPanic(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		data := drawBytes(t, "data", 1024)

		_, _ = ParseServiceRequest(data)
		_, _ = ParseUserAuthRequest(data)
		_, _ = ParseGlobalRequest(data)
		_, _ = ParseChannelOpen(data)
		_, _ = ParseChannelData(data)
		_, _ = ParseProtocolVersion(data)
		_, _ = ParseKeepaliveRequest(data)
		_, _ = ParseKeepaliveOptions(data)
		_, _, _ = ParseForwardData(data)
		_, _, _ = ReadMessage(bytes.NewReader(data))
	})
}

// TestProperty_ReorderIntelGUID_IsPermutation asserts the Intel mixed-endian
// GUID reorder only permutes bytes — the output multiset equals the input
// multiset — so no byte is dropped, duplicated, or invented.
func TestProperty_ReorderIntelGUID_IsPermutation(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		var raw [16]byte
		copy(raw[:], rapid.SliceOfN(rapid.Byte(), 16, 16).Draw(t, "guid"))

		u := ReorderIntelGUID(raw)
		require.ElementsMatch(t, raw[:], u[:])
	})
}
