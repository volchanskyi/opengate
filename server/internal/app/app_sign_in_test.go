package app_test

import (
	"context"
	"crypto/rand"
	"crypto/sha512"
	"io"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/quic-go/quic-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/app"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/rules"
	"github.com/volchanskyi/opengate/server/internal/settings"
	"github.com/volchanskyi/opengate/server/internal/testutil"
	"github.com/volchanskyi/opengate/server/internal/testutil/machinecert"
)

func TestSettingsStoredBeforeTheStartReachTheFirstSignIn(t *testing.T) {
	t.Parallel()

	cfg := baseConfig(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), false)
	site := testutil.SeedSite(t, ctx, cfg.Store)
	machine := testutil.SeedDevice(t, ctx, cfg.Store, site.ID)
	ruleStore := rules.NewStore(cfg.Store.DB())
	pack, err := rules.Embedded()
	require.NoError(t, err)
	tuned := rules.Binding{
		ID: uuid.New(), OrganizationID: site.OrganizationID, RuleID: "disk-critical",
		Level: settings.LevelOrganization, LevelKey: site.OrganizationID,
		Params: map[string]float64{"threshold": 93},
	}
	require.NoError(t, ruleStore.UpsertBinding(ctx, pack, tuned))

	assembly, err := app.Build(context.Background(), cfg)
	require.NoError(t, err)

	// A value written past the product after the start waits for the next start.
	tuned.Params = map[string]float64{"threshold": 95}
	require.NoError(t, ruleStore.UpsertBinding(ctx, pack, tuned))

	pushed := firstRulesAfterSignIn(t, assembly, machine.ID)
	for _, rule := range pushed.AlertRules {
		if rule.ID == "disk-critical" {
			assert.InDelta(t, 93, rule.Threshold, 0.001, "the sign-in reads what the start held")
			return
		}
	}
	t.Fatal("the first sign-in carried no disk-critical rule")
}

// firstRulesAfterSignIn serves the assembly's agent listener, signs machineID in over it, and
// returns the first ruleset the server pushes.
func firstRulesAfterSignIn(t *testing.T, assembly *app.Assembly, machineID uuid.UUID) *protocol.ControlMessage {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	listening := make(chan struct{})
	go func() {
		defer close(listening)
		_ = assembly.Agents.ListenAndServe(ctx, "127.0.0.1:0")
	}()
	t.Cleanup(func() {
		cancel()
		<-listening
	})

	identity := machinecert.Issue(t, assembly.DeviceCerts, machineID)
	conn, err := quic.DialAddr(t.Context(), assembly.Agents.Addr(),
		machinecert.ClientTLS(assembly.Cert, identity), &quic.Config{MaxIdleTimeout: 30 * time.Second})
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.CloseWithError(0, "test done") })
	stream, err := conn.OpenStreamSync(t.Context())
	require.NoError(t, err)

	certHash := sha512.Sum384(identity.Certificate[0])
	var nonce [32]byte
	_, err = rand.Read(nonce[:])
	require.NoError(t, err)
	_, err = stream.Write(protocol.EncodeAgentHello(nonce, certHash))
	require.NoError(t, err)
	_, err = io.ReadFull(stream, make([]byte, 81))
	require.NoError(t, err)

	codec := &protocol.Codec{}
	payload, err := codec.EncodeControl(&protocol.ControlMessage{
		Type:         protocol.MsgAgentRegister,
		Capabilities: []protocol.AgentCapability{protocol.CapThresholdAlerts},
		Hostname:     "contoso-fs01", OS: "linux", Arch: "amd64", Version: "1.0.0",
	})
	require.NoError(t, err)
	require.NoError(t, codec.WriteFrame(stream, protocol.FrameControl, payload))

	require.NoError(t, stream.SetReadDeadline(time.Now().Add(10*time.Second)))
	for {
		frameType, body, err := codec.ReadFrame(stream)
		require.NoError(t, err, "the server sent no ruleset after the sign-in")
		if frameType != protocol.FrameControl {
			continue
		}
		msg, err := codec.DecodeControl(body)
		require.NoError(t, err)
		if msg.Type == protocol.MsgPushAlertRules {
			return msg
		}
	}
}
