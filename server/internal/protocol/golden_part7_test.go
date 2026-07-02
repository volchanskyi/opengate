package protocol

import (
	"bytes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestGoldenControlFileListError(t *testing.T) {
	msg := decodeControlFrame(t, "control_file_list_error.bin")
	assert.Equal(t, MsgFileListError, msg.Type)
	assert.Equal(t, "/root/secret", msg.Path)
	// The Rust FileListError uses `error`; Go's msgpack tag on AckError
	// also binds to `error`, which is how HardwareReportError decodes too.
	assert.Equal(t, "permission denied", msg.AckError)
}

func TestGoldenControlFileDownloadRequest(t *testing.T) {
	msg := decodeControlFrame(t, "control_file_download_request.bin")
	assert.Equal(t, MsgFileDownloadRequest, msg.Type)
	assert.Equal(t, "/home/ivan/notes.txt", msg.Path)
}

func TestGoldenControlFileUploadRequest(t *testing.T) {
	msg := decodeControlFrame(t, "control_file_upload_request.bin")
	assert.Equal(t, MsgFileUploadRequest, msg.Type)
	assert.Equal(t, "/home/ivan/uploads/archive.tar", msg.Path)
	assert.Equal(t, uint64(10_485_760), msg.TotalSize)
}

func TestGoldenControlChatMessage(t *testing.T) {
	msg := decodeControlFrame(t, "control_chat_message.bin")
	assert.Equal(t, MsgChatMessage, msg.Type)
	assert.Equal(t, "hello from the operator", msg.Text)
	assert.Equal(t, "operator@example.com", msg.Sender)
}

func TestGoldenControlAgentRegisterEmptyCapabilities(t *testing.T) {
	msg := decodeControlFrame(t, "control_agent_register_empty_capabilities.bin")
	assert.Equal(t, MsgAgentRegister, msg.Type)
	assert.Equal(t, "headless-ci-runner", msg.Hostname)
	assert.Equal(t, "linux", msg.OS)
	assert.Equal(t, "aarch64", msg.Arch)
	assert.Equal(t, "0.1.0", msg.Version)
	assert.Empty(t, msg.Capabilities, "empty Vec must decode to empty/nil slice, not produce an error")
}

func TestGoldenControlAgentRegisterUTF8(t *testing.T) {
	msg := decodeControlFrame(t, "control_agent_register_utf8.bin")
	assert.Equal(t, MsgAgentRegister, msg.Type)
	// Emoji + CJK must round-trip bit-for-bit through msgpack.
	assert.Equal(t, "ラップトップ-🖥️-办公室", msg.Hostname)
	assert.Equal(t, "macos", msg.OS)
	assert.Equal(t, "aarch64", msg.Arch)
	assert.Equal(t, "0.1.0-αβγ", msg.Version)
	require.Len(t, msg.Capabilities, 1)
	assert.Equal(t, CapRemoteDesktop, msg.Capabilities[0])
}

func TestGoldenControlAgentHealthSummary(t *testing.T) {
	msg := decodeControlFrame(t, "control_agent_health_summary.bin")
	assert.Equal(t, MsgAgentHealthSummary, msg.Type)
	assert.Equal(t, int64(1700000100), msg.TS)
	assert.Equal(t, "00000000-0000-0000-0000-000000000002", msg.OrgID)
	assert.InEpsilon(t, 0.125, msg.NodeAnomalyRate, 0.0001)
	require.Len(t, msg.PerFamilyRates, 2)
	assert.Equal(t, "cpu", msg.PerFamilyRates[0].Family)
	assert.InEpsilon(t, 0.25, msg.PerFamilyRates[0].Rate, 0.0001)
	assert.Equal(t, "process", msg.PerFamilyRates[1].Family)
	assert.InEpsilon(t, 0.5, msg.PerFamilyRates[1].Rate, 0.0001)
	assert.Equal(t, []byte{0xAA, 0x55, 0xF0}, msg.RecentBitmask)
	assert.Equal(t, "sysinfo-k2", msg.SamplerVersion)
	assert.Equal(t, "k2-baseline-v1", msg.ModelVersion)
}

func TestGoldenControlAgentMetricWindow(t *testing.T) {
	msg := decodeControlFrame(t, "control_agent_metric_window.bin")
	assert.Equal(t, MsgAgentMetricWindow, msg.Type)
	assert.Equal(t, int64(1700000160), msg.TS)
	assert.Equal(t, "00000000-0000-0000-0000-000000000002", msg.OrgID)
	require.Len(t, msg.Dims, 2)
	assert.Equal(t, "cpu.total", msg.Dims[0].Name)
	assert.InEpsilon(t, 42.5, msg.Dims[0].Avg, 0.0001)
	assert.Equal(t, "mem.rss", msg.Dims[1].Name)
	assert.InEpsilon(t, 2048.0, msg.Dims[1].Avg, 0.0001)
}

func TestGoldenControlProcessReport(t *testing.T) {
	msg := decodeControlFrame(t, "control_process_report.bin")
	assert.Equal(t, MsgProcessReport, msg.Type)
	assert.Equal(t, int64(1700000220), msg.TS)
	assert.Equal(t, "00000000-0000-0000-0000-000000000002", msg.OrgID)
	require.Len(t, msg.TopN, 1)
	assert.Equal(t, uint32(1), msg.TopN[0].Rank)
	assert.Equal(t, "postgres", msg.TopN[0].Basename)
	require.NotNil(t, msg.TopN[0].CmdlineHash)
	assert.Equal(t, "sha256:abcdef", *msg.TopN[0].CmdlineHash)
	assert.Equal(t, uint32(4242), msg.TopN[0].PID)
	assert.InEpsilon(t, 12.5, msg.TopN[0].CPU, 0.0001)
	assert.InEpsilon(t, 3.25, msg.TopN[0].Mem, 0.0001)
}

func TestGoldenControlRequestHealthWindow(t *testing.T) {
	msg := decodeControlFrame(t, "control_request_health_window.bin")
	assert.Equal(t, MsgRequestHealthWindow, msg.Type)
	assert.Equal(t, int64(1700000000), msg.SinceTS)
	assert.Equal(t, uint32(12), msg.Limit)
}

func TestGoldenControlHealthWindowResponse(t *testing.T) {
	msg := decodeControlFrame(t, "control_health_window_response.bin")
	assert.Equal(t, MsgHealthWindowResponse, msg.Type)
	require.Len(t, msg.Summaries, 1)
	assert.Equal(t, int64(1700000100), msg.Summaries[0].TS)
	assert.Equal(t, "00000000-0000-0000-0000-000000000002", msg.Summaries[0].OrgID)
	assert.InEpsilon(t, 0.125, msg.Summaries[0].NodeAnomalyRate, 0.0001)
	require.Len(t, msg.Summaries[0].PerFamilyRates, 1)
	assert.Equal(t, "cpu", msg.Summaries[0].PerFamilyRates[0].Family)
	assert.InEpsilon(t, 0.25, msg.Summaries[0].PerFamilyRates[0].Rate, 0.0001)
	assert.Equal(t, []byte{0xAA, 0x55, 0xF0}, msg.Summaries[0].RecentBitmask)
	assert.Equal(t, "sysinfo-k2", msg.Summaries[0].SamplerVersion)
	assert.Equal(t, "k2-baseline-v1", msg.Summaries[0].ModelVersion)
}

func TestGoldenControlHardwareReportLargeSize(t *testing.T) {
	data := readGolden(t, "control_hardware_report_large_size.bin")
	// Frame length high bytes must be non-zero — proves the 4-byte BE header
	// is actually exercised, not just the low byte.
	require.Greater(t, len(data), 65_536,
		"large_size golden must exceed 64 KiB to exercise BE length high bytes")

	msg := decodeControlFrame(t, "control_hardware_report_large_size.bin")
	assert.Equal(t, MsgHardwareReport, msg.Type)
	assert.Equal(t, "AMD EPYC 7763 64-Core Processor", msg.CPUModel)
	assert.Equal(t, uint32(128), msg.CPUCores)
	assert.Equal(t, uint64(524_288), msg.RAMTotalMB)
	assert.Equal(t, uint64(8_388_608), msg.DiskTotalMB)
	assert.Equal(t, uint64(4_194_304), msg.DiskFreeMB)
	require.Len(t, msg.NetworkInterfaces, 2000)
	assert.Equal(t, "veth0000", msg.NetworkInterfaces[0].Name)
	assert.Equal(t, "veth1999", msg.NetworkInterfaces[1999].Name)
}

func TestGoldenControlChatMessageForwardCompat(t *testing.T) {
	// Forward-compatibility: the payload contains two msgpack keys
	// (future_field, future_number) that Go's ControlMessage struct does not
	// declare. They must be silently ignored — the known fields decode unchanged.
	msg := decodeControlFrame(t, "control_chat_message_forward_compat.bin")
	assert.Equal(t, MsgChatMessage, msg.Type)
	assert.Equal(t, "hello from the future", msg.Text)
	assert.Equal(t, "operator@example.com", msg.Sender)
}

func TestGoldenFrameControlLELength(t *testing.T) {
	// Negative test: length field encoded little-endian instead of big-endian.
	// BE interpretation inflates the declared length past MaxFrameSize, so
	// ReadFrame must return an error rather than hang or over-read.
	data := readGolden(t, "frame_control_le_length.bin")
	codec := &Codec{}
	_, _, err := codec.ReadFrame(bytes.NewReader(data))
	require.Error(t, err, "LE-encoded length must be rejected by the BE-parsing decoder")
	assert.ErrorIs(t, err, ErrFrameTooLarge)
}
