package transport

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// drawBytes draws a byte slice of length in [0, max].
func drawBytes(t *rapid.T, label string, max int) []byte {
	return rapid.SliceOfN(rapid.Byte(), 0, max).Draw(t, label)
}

// roundTripPayload writes one APF message, reads it back, asserts its type and returns the payload.
func roundTripPayload(t *rapid.T, write func(io.Writer) error, want uint8) []byte {
	var buf bytes.Buffer
	require.NoError(t, write(&buf))
	mt, payload, err := ReadMessage(&buf)
	require.NoError(t, err)
	require.Equal(t, want, mt)
	return payload
}

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

func TestProperty_ReorderIntelGUID_IsPermutation(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		var raw [16]byte
		copy(raw[:], rapid.SliceOfN(rapid.Byte(), 16, 16).Draw(t, "guid"))

		u := ReorderIntelGUID(raw)
		require.ElementsMatch(t, raw[:], u[:])
	})
}
