package integration

import (
	"bytes"
	"compress/flate"
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/quic-go/quic-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/msgpack/v5"

	"github.com/volchanskyi/opengate/server/internal/agentapi"
	"github.com/volchanskyi/opengate/server/internal/alerts"
	"github.com/volchanskyi/opengate/server/internal/cert"
	"github.com/volchanskyi/opengate/server/internal/notifications"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"github.com/volchanskyi/opengate/server/internal/rules"
	"github.com/volchanskyi/opengate/server/internal/settings"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

type alertEnv struct {
	*agentTestEnv
	store *alerts.Store
}

func newAlertEnv(t *testing.T) *alertEnv {
	t.Helper()

	base := newAgentTestEnv(t)
	alertStore := alerts.NewStore(base.store.DB())
	catalogue, err := rules.Embedded()
	require.NoError(t, err)

	cm, err := cert.NewManager(t.TempDir())
	require.NoError(t, err)
	srv := agentapi.NewAgentServer(agentapi.AgentServerConfig{
		Cert:          cm,
		Devices:       testutil.NewTestDevices(t, base.store),
		Hardware:      testutil.NewTestHardware(t, base.store),
		DeviceUpdates: testutil.NewTestDeviceUpdates(t, base.store),
		Relay:         relay.NewRelay(testLogger()),
		Notifier:      &notifications.NoopNotifier{},
		Settings:      settings.NewPostgresReader(base.store.DB()),
		AlertStore:    alertStore,
		RuleCatalogue: catalogue,
		Logger:        testLogger(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	listening := make(chan struct{})
	go func() {
		defer close(listening)
		srv.ListenAndServe(ctx, "127.0.0.1:0")
	}()
	addr := srv.Addr()
	t.Cleanup(func() {
		cancel()
		select {
		case <-listening:
		case <-time.After(2 * time.Second):
			t.Log("agent QUIC server did not exit within 2s of cancel")
		}
	})

	return &alertEnv{
		agentTestEnv: &agentTestEnv{
			store:   base.store,
			devices: base.devices,
			certMgr: cm,
			srv:     srv,
			addr:    addr,
			cancel:  cancel,
		},
		store: alertStore,
	}
}

func (e *alertEnv) connectedMachine(t *testing.T) (*quic.Stream, uuid.UUID) {
	t.Helper()

	deviceID, siteID := uuid.New(), uuid.New()
	e.seedDevice(t, deviceID, siteID)
	stream, agentCert := e.dialAgentStream(t, deviceID)
	performClientHandshake(t, stream, agentCert)
	sendAgentRegister(t, stream)
	drainRegisterHardwareRequest(t, stream)
	return stream, deviceID
}

func raise(t *testing.T, stream *quic.Stream, change func(*protocol.ControlMessage)) {
	t.Helper()

	packed, err := msgpack.Marshal(protocol.AlertEvidence{
		Ranked: []protocol.RankedDim{{Dim: "disk.used_percent", Score: 0.94}},
		Processes: []protocol.ProcessReportEntry{
			{Rank: 1, Basename: "pg_dump", PID: 4242, CPU: 88.0},
		},
	})
	require.NoError(t, err)
	var compressed bytes.Buffer
	writer, err := flate.NewWriter(&compressed, flate.BestSpeed)
	require.NoError(t, err)
	_, err = writer.Write(packed)
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	severity := protocol.AlertSeverityCritical
	backfilled := false
	value := 91.4
	// The wire carries whole seconds, so the machine floors the window to them.
	end := time.Now().UTC().Truncate(time.Second).Add(-time.Minute)
	msg := &protocol.ControlMessage{
		Type:          protocol.MsgAgentAlert,
		AlertID:       uuid.NewString(),
		RuleID:        "disk-critical",
		RuleVersion:   1,
		Severity:      &severity,
		Metric:        "disk.used_percent",
		Value:         &value,
		WindowStartTS: end.Add(-5 * time.Minute).Unix(),
		WindowEndTS:   end.Unix(),
		ObservedTS:    end.Unix(),
		Backfilled:    &backfilled,
		EvidenceCodec: protocol.EvidenceCodec,
		Evidence:      compressed.Bytes(),
	}
	if change != nil {
		change(msg)
	}

	codec := &protocol.Codec{}
	payload, err := codec.EncodeControl(msg)
	require.NoError(t, err)
	require.NoError(t, codec.WriteFrame(stream, protocol.FrameControl, payload))
}

func (e *alertEnv) awaitIncidents(t *testing.T, n int) []alerts.Incident {
	t.Helper()
	ctx := defaultTenantContext()

	var page alerts.Page
	require.Eventuallyf(t, func() bool {
		var err error
		page, err = e.store.Queue(ctx, alerts.Filter{Limit: 50})
		return err == nil && len(page.Incidents) == n
	}, 10*time.Second, 50*time.Millisecond, "expected %d incidents in the queue", n)
	return page.Incidents
}

func (e *alertEnv) queueHolds(oneRoomOneAlert bool) func() bool {
	return func() bool {
		page, err := e.store.Queue(defaultTenantContext(), alerts.Filter{Limit: 50})
		if err != nil {
			return false
		}
		held := len(page.Incidents) == 1 && page.Incidents[0].Occurrences == 1
		return held == oneRoomOneAlert
	}
}

func TestAnAlertAMachineRaisesReachesTheQueue(t *testing.T) {
	t.Parallel()

	env := newAlertEnv(t)
	stream, deviceID := env.connectedMachine(t)

	raise(t, stream, nil)

	rooms := env.awaitIncidents(t, 1)
	room := rooms[0]
	assert.Equal(t, "disk-critical", room.RuleID,
		"the room names the rule the machine said fired")
	assert.Equal(t, alerts.SeverityCritical, room.Severity,
		"and how bad that rule says it is, so the queue can be ordered by it")
	assert.Equal(t, alerts.StatusNew, room.Status, "a new room is the triage queue")
	assert.Equal(t, 1, room.Occurrences)

	opened, err := env.store.Investigation(defaultTenantContext(), room.ID, room.OrganizationID)
	require.NoError(t, err)
	require.Len(t, opened.Alerts, 1)
	assert.Equal(t, deviceID, opened.Alerts[0].DeviceID, "filed against the machine that raised it")
	assert.Equal(t, uint32(1), opened.Alerts[0].RuleVersion,
		"naming the revision of the rule that fired")

	behind, codec, err := env.store.Evidence(defaultTenantContext(), room.ID, opened.Alerts[0].ID)
	require.NoError(t, err)
	assert.NotEmpty(t, behind)
	assert.Equal(t, protocol.EvidenceCodec, codec)
}

func TestTheSameAlertOfferedTwiceStaysOneIncident(t *testing.T) {
	t.Parallel()

	env := newAlertEnv(t)
	stream, _ := env.connectedMachine(t)

	fixed := uuid.New()
	end := time.Now().UTC().Truncate(time.Second).Add(-time.Minute)
	sameAlert := func(msg *protocol.ControlMessage) {
		msg.AlertID = fixed.String()
		msg.WindowStartTS = end.Add(-5 * time.Minute).Unix()
		msg.WindowEndTS = end.Unix()
		msg.ObservedTS = end.Unix()
	}

	raise(t, stream, sameAlert)
	rooms := env.awaitIncidents(t, 1)
	require.Equal(t, 1, rooms[0].Occurrences)

	raise(t, stream, sameAlert)

	require.Never(t, env.queueHolds(false), 2*time.Second, 100*time.Millisecond,
		"a re-delivery must resolve to the row already written, not open a second one")
}

func TestAnAlertThatCannotBeIdentifiedIsRefusedRatherThanGuessedAt(t *testing.T) {
	t.Parallel()

	env := newAlertEnv(t)
	stream, _ := env.connectedMachine(t)

	raise(t, stream, func(msg *protocol.ControlMessage) { msg.RuleVersion = 0 })

	require.Never(t, func() bool {
		page, err := env.store.Queue(defaultTenantContext(), alerts.Filter{Limit: 50})
		return err == nil && len(page.Incidents) > 0
	}, 2*time.Second, 100*time.Millisecond,
		"an alert nothing can identify must not become a room")

	raise(t, stream, nil)
	env.awaitIncidents(t, 1)
}

func TestAnAlertIsFiledUnderTheCustomerTheMachineBelongsTo(t *testing.T) {
	t.Parallel()

	env := newAlertEnv(t)
	stream, _ := env.connectedMachine(t)

	raise(t, stream, nil)
	rooms := env.awaitIncidents(t, 1)

	assert.NotEqual(t, uuid.Nil, rooms[0].OrganizationID,
		"every scoping key an incident is built on is the customer's, resolved on the server")
	assert.Equal(t, alerts.ScopeDevice, rooms[0].Scope,
		"disk-critical groups on the machine and its mount, so the room is about that machine")
}
