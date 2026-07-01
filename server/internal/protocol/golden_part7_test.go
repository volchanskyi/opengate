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
