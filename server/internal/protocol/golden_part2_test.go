package protocol

import (
	"bytes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestGoldenFrameWireFormat(t *testing.T) {
	// All framed golden files share the wire format: [type][4-byte-BE-length][msgpack-payload]
	tests := []struct {
		name         string
		file         string
		expectedType byte
	}{
		{"AgentRegister", "control_agent_register.bin", FrameControl},
		{"Heartbeat", "control_heartbeat.bin", FrameControl},
		{"RelayReady", "control_relay_ready.bin", FrameControl},
		{"DesktopFrame", "desktop_frame.bin", FrameDesktop},
		{"SwitchToWebRTC", "control_switch_to_webrtc.bin", FrameControl},
		{"IceCandidate", "control_ice_candidate.bin", FrameControl},
		{"SwitchAck", "control_switch_ack.bin", FrameControl},
		{"AgentUpdateAck", "control_agent_update_ack.bin", FrameControl},
		{"AgentDeregistered", "control_agent_deregistered.bin", FrameControl},
		{"RestartAgent", "control_restart_agent.bin", FrameControl},
		{"RequestHardwareReport", "control_request_hardware_report.bin", FrameControl},
		{"HardwareReport", "control_hardware_report.bin", FrameControl},
		{"HardwareReportError", "control_hardware_report_error.bin", FrameControl},
		{"RequestDeviceLogs", "control_request_device_logs.bin", FrameControl},
		{"DeviceLogsResponse", "control_device_logs_response.bin", FrameControl},
		{"DeviceLogsError", "control_device_logs_error.bin", FrameControl},
		{"SessionAccept", "control_session_accept.bin", FrameControl},
		{"SessionReject", "control_session_reject.bin", FrameControl},
		{"SessionRequest", "control_session_request.bin", FrameControl},
		{"AgentUpdate", "control_agent_update.bin", FrameControl},
		{"FileListRequest", "control_file_list_request.bin", FrameControl},
		{"FileListResponse", "control_file_list_response.bin", FrameControl},
		{"FileListError", "control_file_list_error.bin", FrameControl},
		{"FileDownloadRequest", "control_file_download_request.bin", FrameControl},
		{"FileUploadRequest", "control_file_upload_request.bin", FrameControl},
		{"ChatMessage", "control_chat_message.bin", FrameControl},
		{"AgentRegisterEmptyCaps", "control_agent_register_empty_capabilities.bin", FrameControl},
		{"AgentRegisterUTF8", "control_agent_register_utf8.bin", FrameControl},
		{"HardwareReportLargeSize", "control_hardware_report_large_size.bin", FrameControl},
		{"ChatMessageForwardCompat", "control_chat_message_forward_compat.bin", FrameControl},
		{"UnknownFutureAgentToServer", "control_unknown_future_agent_to_server.bin", FrameControl},
	}

	codec := &Codec{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := readGolden(t, tt.file)

			// Verify frame type byte
			assert.Equal(t, tt.expectedType, data[0], "frame type mismatch")

			// Verify we can read the frame
			reader := bytes.NewReader(data)
			frameType, payload, err := codec.ReadFrame(reader)
			require.NoError(t, err)
			assert.Equal(t, tt.expectedType, frameType)
			assert.NotEmpty(t, payload, "payload should not be empty")

			// For control frames, verify we can decode the msgpack payload
			if frameType == FrameControl {
				msg, err := codec.DecodeControl(payload)
				require.NoError(t, err, "failed to decode control message from golden file")
				assert.NotEmpty(t, msg.Type)
			}
		})
	}
}

func TestGoldenControlUnknownFutureAgentToServer(t *testing.T) {
	data := readGolden(t, "control_unknown_future_agent_to_server.bin")
	codec := &Codec{}

	reader := bytes.NewReader(data)
	frameType, payload, err := codec.ReadFrame(reader)
	require.NoError(t, err)
	assert.Equal(t, FrameControl, frameType)

	msg, err := codec.DecodeControl(payload)
	require.NoError(t, err)
	assert.Equal(t, ControlMessageType("FutureTelemetryWindow"), msg.Type)
}
