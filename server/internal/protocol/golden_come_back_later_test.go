package protocol

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGoldenControlComeBackLater(t *testing.T) {
	msg := decodeControlFrame(t, "control_come_back_later.bin")

	assert.Equal(t, &ControlMessage{Type: MsgComeBackLater, RetryAfter: 17}, msg)
}

func TestGenerateComeBackLaterReverseGoldens(t *testing.T) {
	dir := goldenWriteDir(t)
	codec := &Codec{}

	writeReverseControlFrame(t, dir, codec, "control_come_back_later", &ControlMessage{
		Type:       MsgComeBackLater,
		RetryAfter: 17,
	})
	// A wait of nought is dropped from the wire map, which the agent reads as at once.
	writeReverseControlFrame(t, dir, codec, "control_come_back_later_min", &ControlMessage{
		Type: MsgComeBackLater,
	})

	if !shouldGenerateGolden() {
		for _, variant := range []string{"control_come_back_later", "control_come_back_later_min"} {
			name := "go_" + variant + ".bin"
			require.Equal(t, readGolden(t, name), readFile(t, dir, name),
				"%s drifted from what the encoder writes; regenerate with GENERATE_GOLDEN=1", name)
		}
	}
}
