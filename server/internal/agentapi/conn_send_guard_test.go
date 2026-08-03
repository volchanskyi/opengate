package agentapi

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// The agent decodes SessionRequest and AgentUpdate with every load-bearing
// field required, and the server's encoder drops a zero-valued field from the
// wire map. An empty token, relay URL, version, URL or signature therefore
// produces a frame the agent cannot decode — which drops its control stream.
// These tests pin that such a frame never leaves the server.

func TestSendSessionRequest_RefusesUndecodableFrame(t *testing.T) {
	t.Parallel()
	token := protocol.GenerateSessionToken()
	perms := protocol.Permissions{Desktop: true, Terminal: true}

	tests := []struct {
		name      string
		token     protocol.SessionToken
		relayURL  string
		wantField string
	}{
		{"empty token", protocol.SessionToken(""), "wss://relay/test", "token"},
		{"empty relay url", token, "", "relay_url"},
		{"both empty", protocol.SessionToken(""), "", "token"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ac, buf := newTestAgentConn(t, uuid.New(), nil)

			err := ac.SendSessionRequest(context.Background(), tt.token, tt.relayURL, perms)

			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrIncompleteControlMessage), "want ErrIncompleteControlMessage, got %v", err)
			assert.True(t, IsIncompleteMessageError(err))
			assert.Contains(t, err.Error(), tt.wantField)
			assert.Contains(t, err.Error(), string(protocol.MsgSessionRequest))
			assert.Zero(t, buf.Len(), "no frame may reach the agent")
		})
	}
}

func TestSendSessionRequest_SendsCompleteFrame(t *testing.T) {
	t.Parallel()
	ac, buf := newTestAgentConn(t, uuid.New(), nil)
	token := protocol.GenerateSessionToken()

	require.NoError(t, ac.SendSessionRequest(context.Background(), token, "wss://relay/test",
		protocol.Permissions{Desktop: true}))

	_, payload, err := ac.codec.ReadFrame(buf)
	require.NoError(t, err)
	decoded, err := ac.codec.DecodeControl(payload)
	require.NoError(t, err)
	assert.Equal(t, token, decoded.Token)
	assert.Equal(t, "wss://relay/test", decoded.RelayURL)
}

func TestSendAgentUpdate_RefusesUndecodableFrame(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		version   string
		url       string
		signature string
		wantField string
	}{
		{"empty version", "", "https://example.com/agent", "sig123", "version"},
		{"empty url", "0.3.0", "", "sig123", "url"},
		{"unsigned", "0.3.0", "https://example.com/agent", "", "signature"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ac, buf := newTestAgentConn(t, uuid.New(), nil)

			err := ac.SendAgentUpdate(context.Background(), tt.version, tt.url, "sha256hash", tt.signature)

			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrIncompleteControlMessage), "want ErrIncompleteControlMessage, got %v", err)
			assert.True(t, IsIncompleteMessageError(err))
			assert.Contains(t, err.Error(), tt.wantField)
			assert.Contains(t, err.Error(), string(protocol.MsgAgentUpdate))
			assert.Zero(t, buf.Len(), "no frame may reach the agent")
		})
	}
}

// An absent sha256 is the one AgentUpdate field the agent defaults at decode:
// it is verified against the downloaded artifact by the updater, so it fails
// closed at install time rather than at decode time.
func TestSendAgentUpdate_AllowsEmptySHA256(t *testing.T) {
	t.Parallel()
	ac, buf := newTestAgentConn(t, uuid.New(), nil)

	require.NoError(t, ac.SendAgentUpdate(context.Background(), "0.3.0", "https://example.com/agent", "", "sig123"))

	_, payload, err := ac.codec.ReadFrame(buf)
	require.NoError(t, err)
	decoded, err := ac.codec.DecodeControl(payload)
	require.NoError(t, err)
	assert.Equal(t, protocol.MsgAgentUpdate, decoded.Type)
	assert.Empty(t, decoded.SHA256)
	assert.Equal(t, "sig123", decoded.Signature)
}

func TestIsIncompleteMessageError(t *testing.T) {
	t.Parallel()
	assert.True(t, IsIncompleteMessageError(ErrIncompleteControlMessage))
	assert.False(t, IsIncompleteMessageError(nil))
	assert.False(t, IsIncompleteMessageError(errors.New("some other failure")))
	assert.False(t, IsIncompleteMessageError(ErrCapabilityNotAdvertised))
	// The two classifiers stay disjoint so a handler can tell a capability gap
	// from an undeliverable frame.
	assert.False(t, IsCapabilityError(ErrIncompleteControlMessage))
}

func TestRequireNonEmptyFields_ReportsFirstEmptyField(t *testing.T) {
	t.Parallel()

	assert.NoError(t, requireNonEmptyFields(protocol.MsgAgentUpdate,
		controlField{"version", "0.3.0"}, controlField{"url", "https://x"}))

	err := requireNonEmptyFields(protocol.MsgAgentUpdate,
		controlField{"version", ""}, controlField{"url", ""})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "version")
	assert.NotContains(t, err.Error(), "url")
}
