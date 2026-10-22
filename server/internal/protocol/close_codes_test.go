package protocol

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCloseCodesMatchTheSharedTable(t *testing.T) {
	var table map[string]uint64
	require.NoError(t, json.Unmarshal(readGolden(t, "close_codes.json"), &table))

	require.Equal(t, map[string]uint64{
		"bye":                          CloseBye,
		"stream_accept_failed":         CloseStreamAcceptFailed,
		"handshake_refused":            CloseHandshakeRefused,
		"device_deleted":               CloseDeviceDeleted,
		"restricted_session_ended":     CloseRestrictedSessionEnded,
		"certificate_refused":          CloseCertificateRefused,
		"replaced_by_newer_connection": CloseReplacedByNewerConnection,
	}, table)
}
