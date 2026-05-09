package mps

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rawAPFString builds wire bytes for a length-prefixed string without
// clamping, so callers can exercise the maxAPFStringLen boundary check
// in the read* helpers.
func rawAPFString(strLen uint32, body []byte) []byte {
	out := make([]byte, 4+len(body))
	binary.BigEndian.PutUint32(out[:4], strLen)
	copy(out[4:], body)
	return out
}

// TestAPFRead_StringLenBoundaries pins the `strLen > maxAPFStringLen` checks
// at lines 472, 492, 512, 543, 561, and 594. Each branch is exercised at
// strLen == maxAPFStringLen (must succeed) and strLen == maxAPFStringLen+1
// (must error). Without these tests, CONDITIONALS_BOUNDARY mutants on
// `>` flip to `>=` and survive whenever the test only uses a safely-short
// string.
func TestAPFRead_StringLenBoundaries(t *testing.T) {
	const max = uint32(maxAPFStringLen)
	const tooLong = max + 1

	exactBody := bytes.Repeat([]byte("a"), int(max))

	cases := []struct {
		name      string
		msgType   uint8
		buildOK   func() []byte // must succeed at strLen == max
		buildBad  func() []byte // must fail at strLen == tooLong
		errSubstr string
	}{
		{
			name:    "service_request",
			msgType: APFServiceRequest,
			buildOK: func() []byte {
				return rawAPFString(max, exactBody)
			},
			buildBad: func() []byte {
				return rawAPFString(tooLong, nil) // header only; rejected before body read
			},
			errSubstr: "service name too long",
		},
		{
			name:    "user_auth_request",
			msgType: APFUserAuthRequest,
			buildOK: func() []byte {
				ok := rawAPFString(max, exactBody)
				ok = append(ok, rawAPFString(uint32(len(ServiceAuth)), []byte(ServiceAuth))...)
				ok = append(ok, rawAPFString(uint32(len("digest")), []byte("digest"))...)
				return ok
			},
			buildBad: func() []byte {
				return rawAPFString(tooLong, nil)
			},
			errSubstr: "auth string too long",
		},
		{
			name:    "global_request_name_len",
			msgType: APFGlobalRequest,
			buildOK: func() []byte {
				// max-length name + want_reply byte. No tcpip-forward extras.
				ok := rawAPFString(max, exactBody)
				ok = append(ok, 0)
				return ok
			},
			buildBad: func() []byte {
				return rawAPFString(tooLong, nil)
			},
			errSubstr: "request name too long",
		},
		{
			name:    "global_request_forward_addr_len",
			msgType: APFGlobalRequest,
			buildOK: func() []byte {
				// "tcpip-forward" name + want_reply + max-length addr + port
				name := "tcpip-forward"
				ok := rawAPFString(uint32(len(name)), []byte(name))
				ok = append(ok, 1)
				ok = append(ok, rawAPFString(max, exactBody)...)
				ok = append(ok, encodeUint32(16993)...)
				return ok
			},
			buildBad: func() []byte {
				name := "tcpip-forward"
				bad := rawAPFString(uint32(len(name)), []byte(name))
				bad = append(bad, 1)
				bad = append(bad, rawAPFString(tooLong, nil)...)
				return bad
			},
			errSubstr: "forward address too long",
		},
		{
			name:    "channel_open_type_len",
			msgType: APFChannelOpen,
			buildOK: func() []byte {
				ok := rawAPFString(max, exactBody)
				ok = append(ok, encodeUint32(1)...)      // sender
				ok = append(ok, encodeUint32(0x8000)...) // window
				ok = append(ok, encodeUint32(0x8000)...) // max packet
				return ok
			},
			buildBad: func() []byte {
				return rawAPFString(tooLong, nil)
			},
			errSubstr: "channel type too long",
		},
		{
			name:    "channel_open_extra_addr_len",
			msgType: APFChannelOpen,
			buildOK: func() []byte {
				// "direct-tcpip" triggers readChannelOpenExtra; first extra
				// string is allowed at exactly maxAPFStringLen.
				name := "direct-tcpip"
				ok := rawAPFString(uint32(len(name)), []byte(name))
				ok = append(ok, encodeUint32(1)...)
				ok = append(ok, encodeUint32(0x8000)...)
				ok = append(ok, encodeUint32(0x8000)...)
				ok = append(ok, rawAPFString(max, exactBody)...)
				ok = append(ok, encodeUint32(80)...)
				ok = append(ok, rawAPFString(max, exactBody)...)
				ok = append(ok, encodeUint32(80)...)
				return ok
			},
			buildBad: func() []byte {
				name := "direct-tcpip"
				bad := rawAPFString(uint32(len(name)), []byte(name))
				bad = append(bad, encodeUint32(1)...)
				bad = append(bad, encodeUint32(0x8000)...)
				bad = append(bad, encodeUint32(0x8000)...)
				bad = append(bad, rawAPFString(tooLong, nil)...)
				return bad
			},
			errSubstr: "address too long",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name+"/exact_max_ok", func(t *testing.T) {
			buf := bytes.NewBuffer([]byte{tc.msgType})
			buf.Write(tc.buildOK())
			gotType, _, err := ReadMessage(buf)
			require.NoError(t, err, "max-length string must be accepted, not error")
			assert.Equal(t, tc.msgType, gotType)
		})
		t.Run(tc.name+"/over_max_errors", func(t *testing.T) {
			buf := bytes.NewBuffer([]byte{tc.msgType})
			buf.Write(tc.buildBad())
			_, _, err := ReadMessage(buf)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.errSubstr)
		})
	}
}

// TestParseGlobalRequest_OffsetBoundary pins ParseGlobalRequest at the
// `off >= len(data)` boundary (apf.go:184). Without an exact-fit input,
// the CONDITIONALS_BOUNDARY mutation `>=` → `>` survives.
func TestParseGlobalRequest_OffsetBoundary(t *testing.T) {
	// name="x", 4-byte len + 1-byte string = 5 bytes; no want_reply byte → off==len(data).
	data := rawAPFString(1, []byte("x"))
	require.Len(t, data, 5)
	_, err := ParseGlobalRequest(data)
	require.ErrorIs(t, err, ErrMessageTooShort)

	// One more byte: off=5, len(data)=6 → success.
	full := append([]byte(nil), data...)
	full = append(full, 1)
	gr, err := ParseGlobalRequest(full)
	require.NoError(t, err)
	assert.Equal(t, "x", gr.RequestName)
	assert.True(t, gr.WantReply)
}

// TestParseChannelOpen_OffsetBoundary pins ParseChannelOpen at the
// `off+12 > len(data)` boundary (apf.go:211).
func TestParseChannelOpen_OffsetBoundary(t *testing.T) {
	// name="ch", off after readString = 6. Need 12 more bytes for header.
	name := []byte("ch")
	header := rawAPFString(uint32(len(name)), name)
	// 11 bytes of header data → off+12 == len(data)+1 → must error.
	short := append([]byte(nil), header...)
	short = append(short, make([]byte, 11)...)
	_, err := ParseChannelOpen(short)
	require.ErrorIs(t, err, ErrMessageTooShort)

	// Exactly 12 bytes → off+12 == len(data) → must succeed.
	exact := append([]byte(nil), header...)
	exact = append(exact, make([]byte, 12)...)
	co, err := ParseChannelOpen(exact)
	require.NoError(t, err)
	assert.Equal(t, "ch", co.ChannelType)
}

// TestParseChannelData_LenBoundary pins ParseChannelData at `len(data) < 4`
// (apf.go:235).
//   - 3 bytes: bare ErrMessageTooShort.
//   - 4 bytes: passes the length gate, then readString fails — error is
//     wrapped under "data payload: ...". This distinguishes the original
//     `<` from the mutated `<=`, which would short-circuit to bare
//     ErrMessageTooShort instead.
//   - 8 bytes (ch + zero-len string): success.
func TestParseChannelData_LenBoundary(t *testing.T) {
	// 8 bytes: 4 ch + 4 dataLen(0) → success.
	exact := make([]byte, 8)
	cd, err := ParseChannelData(exact)
	require.NoError(t, err)
	assert.Equal(t, uint32(0), cd.RecipientChannel)
	assert.Empty(t, cd.Data)

	// 3 bytes: under the gate → bare ErrMessageTooShort.
	_, err = ParseChannelData(make([]byte, 3))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrMessageTooShort)
	assert.NotContains(t, err.Error(), "data payload",
		"3-byte input must hit the length gate, not the readString path")

	// 4 bytes (exact boundary): the mutation `<` → `<=` would short-circuit
	// here. Original passes the gate and readString fails downstream, so the
	// error is wrapped under "data payload".
	_, err = ParseChannelData(make([]byte, 4))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrMessageTooShort)
	assert.Contains(t, err.Error(), "data payload",
		"4-byte input must pass the length gate and fail in readString")
}

// TestWriteChannelData_PayloadCap pins the `len(data) > maxAPFPayload`
// check at apf.go:304. Writing exactly maxAPFPayload bytes must succeed;
// one byte over must error.
func TestWriteChannelData_PayloadCap(t *testing.T) {
	exact := make([]byte, maxAPFPayload)
	var buf bytes.Buffer
	require.NoError(t, WriteChannelData(&buf, 1, exact))

	tooBig := make([]byte, maxAPFPayload+1)
	err := WriteChannelData(io.Discard, 1, tooBig)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "channel data too large")
}

// TestReadChannelData_LenCap pins the `dataLen > 1<<20` safety limit at
// apf.go:615. Exactly 1 MiB must succeed; one byte over must error.
func TestReadChannelData_LenCap(t *testing.T) {
	// 1 MiB exact: header (1 type + 4 ch + 4 len) + 1<<20 bytes.
	const limit = uint32(1 << 20)
	body := make([]byte, limit)
	wire := []byte{APFChannelData}
	wire = append(wire, encodeUint32(1)...) // ch
	wire = append(wire, encodeUint32(limit)...)
	wire = append(wire, body...)
	mt, raw, err := ReadMessage(bytes.NewReader(wire))
	require.NoError(t, err)
	assert.Equal(t, APFChannelData, mt)
	assert.Equal(t, 8+int(limit), len(raw))

	// One past the limit: must error.
	wireBad := []byte{APFChannelData}
	wireBad = append(wireBad, encodeUint32(1)...)
	wireBad = append(wireBad, encodeUint32(limit+1)...)
	_, _, err = ReadMessage(bytes.NewReader(wireBad))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "channel data too large")
}

// TestWriteStringMsg_LenCap pins writeStringMsg at the `len(s) > maxAPFStringLen`
// boundary (apf.go:635). Writing maxAPFStringLen-byte service must succeed;
// writing one byte over must error.
func TestWriteStringMsg_LenCap(t *testing.T) {
	exact := string(bytes.Repeat([]byte("a"), maxAPFStringLen))
	var buf bytes.Buffer
	require.NoError(t, WriteServiceAccept(&buf, exact))

	tooBig := exact + "a"
	err := WriteServiceAccept(io.Discard, tooBig)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "string too long")
}

// TestReadGlobalRequest_NameDispatch pins the
// `reqName == "tcpip-forward" || reqName == "cancel-tcpip-forward"`
// check (apf.go:526). A non-tcpip name must NOT trigger readForwardData,
// while "tcpip-forward" must. CONDITIONALS_NEGATION on either == flips
// the dispatch, which without these assertions survives.
func TestReadGlobalRequest_NameDispatch(t *testing.T) {
	// 1) Unknown name + want_reply byte only → no extra data parsed.
	wire := []byte{APFGlobalRequest}
	wire = append(wire, rawAPFString(7, []byte("unknown"))...)
	wire = append(wire, 0)
	mt, raw, err := ReadMessage(bytes.NewReader(wire))
	require.NoError(t, err)
	assert.Equal(t, APFGlobalRequest, mt)
	gr, err := ParseGlobalRequest(raw)
	require.NoError(t, err)
	assert.Equal(t, "unknown", gr.RequestName)
	assert.Empty(t, gr.Data, "non-tcpip name must not consume forward data")

	// 2) "tcpip-forward" → forward data IS consumed.
	wire2 := []byte{APFGlobalRequest}
	wire2 = append(wire2, rawAPFString(13, []byte("tcpip-forward"))...)
	wire2 = append(wire2, 1)
	wire2 = append(wire2, rawAPFString(7, []byte("0.0.0.0"))...)
	wire2 = append(wire2, encodeUint32(16993)...)
	mt2, raw2, err := ReadMessage(bytes.NewReader(wire2))
	require.NoError(t, err)
	assert.Equal(t, APFGlobalRequest, mt2)
	gr2, err := ParseGlobalRequest(raw2)
	require.NoError(t, err)
	assert.Equal(t, "tcpip-forward", gr2.RequestName)
	assert.NotEmpty(t, gr2.Data)
}

// TestReadChannelOpen_TypeDispatch pins the
// `chType == "direct-tcpip" || chType == "forwarded-tcpip"` check
// (apf.go:575). A non-tcpip type must NOT trigger readChannelOpenExtra.
func TestReadChannelOpen_TypeDispatch(t *testing.T) {
	// "session" → readChannelOpenExtra NOT called. Header only.
	wire := []byte{APFChannelOpen}
	wire = append(wire, rawAPFString(7, []byte("session"))...)
	wire = append(wire, encodeUint32(1)...)
	wire = append(wire, encodeUint32(0x8000)...)
	wire = append(wire, encodeUint32(0x8000)...)
	mt, raw, err := ReadMessage(bytes.NewReader(wire))
	require.NoError(t, err)
	assert.Equal(t, APFChannelOpen, mt)
	co, err := ParseChannelOpen(raw)
	require.NoError(t, err)
	assert.Equal(t, "session", co.ChannelType)
	assert.Empty(t, co.Data, "session type must not consume extra data")

	// "direct-tcpip" → readChannelOpenExtra called.
	wire2 := []byte{APFChannelOpen}
	wire2 = append(wire2, rawAPFString(12, []byte("direct-tcpip"))...)
	wire2 = append(wire2, encodeUint32(1)...)
	wire2 = append(wire2, encodeUint32(0x8000)...)
	wire2 = append(wire2, encodeUint32(0x8000)...)
	wire2 = append(wire2, rawAPFString(7, []byte("0.0.0.0"))...)
	wire2 = append(wire2, encodeUint32(80)...)
	wire2 = append(wire2, rawAPFString(7, []byte("1.2.3.4"))...)
	wire2 = append(wire2, encodeUint32(81)...)
	mt2, raw2, err := ReadMessage(bytes.NewReader(wire2))
	require.NoError(t, err)
	assert.Equal(t, APFChannelOpen, mt2)
	co2, err := ParseChannelOpen(raw2)
	require.NoError(t, err)
	assert.Equal(t, "direct-tcpip", co2.ChannelType)
	assert.NotEmpty(t, co2.Data)
}

// TestReadString_OffsetBoundary pins readString at both boundary checks:
// `offset+4 > len(data)` (apf.go:447) and `offset+length > len(data)`
// (apf.go:452). These are private — exercise via ParseGlobalRequest, which
// calls readString as its first step.
func TestReadString_OffsetBoundary(t *testing.T) {
	// Length prefix at offset+4 == len(data): only 4-byte len with strLen=0,
	// then no want_reply → boundary hit downstream but readString itself
	// must succeed at offset=0, len(data)=4 (returns empty string).
	exact := make([]byte, 5) // 4 bytes len=0 + 1 byte want_reply
	gr, err := ParseGlobalRequest(exact)
	require.NoError(t, err)
	assert.Equal(t, "", gr.RequestName)

	// Only 3 bytes → offset+4 > len(data) → readString errors.
	_, err = ParseGlobalRequest(make([]byte, 3))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrMessageTooShort)

	// length=2 declared but only 1 body byte → offset+length > len(data).
	bad := []byte{0, 0, 0, 2, 'a'}
	_, err = ParseGlobalRequest(bad)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrMessageTooShort)
}
