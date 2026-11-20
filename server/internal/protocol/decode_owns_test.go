package protocol

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestADecodedMessageKeepsNothingOfThePayload(t *testing.T) {
	sent := &ControlMessage{
		Type:          MsgAgentAlert,
		RuleID:        "disk-critical",
		Evidence:      []byte{0x78, 0x9c, 0x01, 0x02, 0x03},
		CSR:           []byte{0x30, 0x81, 0x9a},
		Cert:          []byte{0x30, 0x82, 0x01},
		RecentBitmask: []byte{0xff, 0x0f},
		Hostname:      "dal-fs-01",
		BackfillSamples: []BackfillSample{
			{Name: "cpu.total", TS: 1_700_000_000, Value: 42.5},
		},
	}
	codec := &Codec{}
	payload, err := codec.EncodeControl(sent)
	require.NoError(t, err)
	decoded, err := codec.DecodeControl(payload)
	require.NoError(t, err)
	want, err := codec.DecodeControl(append([]byte(nil), payload...))
	require.NoError(t, err)

	for i := range payload {
		payload[i] = 0xaa
	}

	assert.Equal(t, want, decoded, "the payload's buffer is free for the next frame once decoded")
}
