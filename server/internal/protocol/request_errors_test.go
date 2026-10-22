package protocol

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRequestErrorsMatchTheSharedTable(t *testing.T) {
	var table map[string]string
	require.NoError(t, json.Unmarshal(readGolden(t, "request_errors.json"), &table))

	require.Equal(t, map[string]string{
		"busy":      RequestBusy,
		"timed_out": RequestTimedOut,
	}, table)
}
