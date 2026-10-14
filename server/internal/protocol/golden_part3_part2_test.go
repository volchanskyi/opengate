package protocol

import (
	"bytes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestGoldenControlIceCandidate(t *testing.T) {
	data := readGolden(t, "control_ice_candidate.bin")
	codec := &Codec{}

	reader := bytes.NewReader(data)
	frameType, payload, err := codec.ReadFrame(reader)
	require.NoError(t, err)
	assert.Equal(t, FrameControl, frameType)

	msg, err := codec.DecodeControl(payload)
	require.NoError(t, err)
	assert.Equal(t, MsgIceCandidate, msg.Type)
	assert.Equal(t, "candidate:1 1 UDP 2130706431 192.168.1.1 50000 typ host", msg.Candidate)
	assert.Equal(t, "0", msg.Mid)
}

func TestGoldenControlDirectLinkNotices(t *testing.T) {
	refused := decodeControlFrame(t, "control_direct_refused.bin")
	assert.Equal(t, MsgDirectRefused, refused.Type)
	assert.Equal(t, "a direct link is already open for this session", refused.Reason)

	assert.Equal(t, MsgDirectSwitch, decodeControlFrame(t, "control_direct_switch.bin").Type)

	fallback := decodeControlFrame(t, "control_direct_fallback.bin")
	assert.Equal(t, MsgDirectFallback, fallback.Type)
	assert.Equal(t, uint64(70_000), fallback.Received)

	none := decodeControlFrame(t, "control_direct_fallback_min.bin")
	assert.Equal(t, MsgDirectFallback, none.Type)
	assert.Zero(t, none.Received)

	ack := decodeControlFrame(t, "control_direct_ack.bin")
	assert.Equal(t, MsgDirectAck, ack.Type)
	assert.Equal(t, uint64(4_294_967_296), ack.Received)
}

func TestGoldenControlFileDownloadErrorAndListingPage(t *testing.T) {
	failed := decodeControlFrame(t, "control_file_download_error.bin")
	assert.Equal(t, MsgFileDownloadError, failed.Type)
	assert.Equal(t, "/var/log/huge.log", failed.Path)
	assert.Equal(t, "a download is already running in this session", failed.AckError)

	page := decodeControlFrame(t, "control_file_list_response_page.bin")
	assert.Equal(t, MsgFileListResponse, page.Type)
	assert.True(t, page.More)
	require.Len(t, page.Entries, 1)
	assert.Equal(t, "syslog", page.Entries[0].Name)

	last := decodeControlFrame(t, "control_file_list_response.bin")
	assert.False(t, last.More)
}

func TestGoldenControlSessionRequestCarriesHelpers(t *testing.T) {
	withHelpers := decodeControlFrame(t, "control_session_request.bin")
	assert.Equal(t, []string{"stun:relay.example.com:9090"}, withHelpers.IceServers)

	without := decodeControlFrame(t, "control_session_request_no_helpers.bin")
	assert.Empty(t, without.IceServers)
}
