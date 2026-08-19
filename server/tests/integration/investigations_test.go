package integration

import (
	"bytes"
	"compress/flate"
	"context"
	"log/slog"
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
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/notifications"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"github.com/volchanskyi/opengate/server/internal/rules"
	"github.com/volchanskyi/opengate/server/internal/settings"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// The whole triage path in one test: a machine raises an alert over its own QUIC
// control stream, the alert lands with the evidence it carried, a room opens for
// it, and a technician closes that room with a cause code.
//
// Every leg of this has unit coverage of its own — admission in
// internal/agentapi, folding and lifecycle in internal/alerts, the workspace in
// web/e2e/investigations.spec.ts. What none of them can show is that the legs
// join up: the alert a real agent encodes is the row the store files, the room
// the fold opens is the one the queue hands a technician, and the resolution
// that closes it is the same room. A seam that only ever holds under a fake is a
// seam nobody has tested.

const triageRule = "cpu-saturated"

// investigationsEnv is an agent-facing server wired the way production wires
// one for alerts: the real store, the compiled-in catalogue, and a settings
// reader that resolves which customer a machine belongs to.
type investigationsEnv struct {
	*agentTestEnv
	alerts *alerts.Store
}

func newInvestigationsEnv(t *testing.T) *investigationsEnv {
	t.Helper()

	store := testutil.NewTestStore(t)
	cm, err := cert.NewManager(t.TempDir())
	require.NoError(t, err)
	catalogue, err := rules.Embedded()
	require.NoError(t, err)

	alertStore := alerts.NewStore(store.DB())
	srv := agentapi.NewAgentServer(agentapi.AgentServerConfig{
		Cert:          cm,
		Devices:       testutil.NewTestDevices(t, store),
		Hardware:      testutil.NewTestHardware(t, store),
		DeviceUpdates: testutil.NewTestDeviceUpdates(t, store),
		Relay:         relay.NewRelay(slog.Default()),
		Notifier:      &notifications.NoopNotifier{},
		Logger:        testLogger(),
		Settings:      settings.NewPostgresReader(store.DB()),
		AlertStore:    alertStore,
		RuleCatalogue: catalogue,
	})

	ctx, cancel := context.WithCancel(context.Background())
	listenDone := make(chan struct{})
	go func() {
		defer close(listenDone)
		srv.ListenAndServe(ctx, "127.0.0.1:0")
	}()
	addr := srv.Addr()

	t.Cleanup(func() {
		cancel()
		select {
		case <-listenDone:
		case <-time.After(2 * time.Second):
			t.Log("agent QUIC server did not exit within 2s of cancel")
		}
	})

	return &investigationsEnv{
		agentTestEnv: &agentTestEnv{
			store:   store,
			certMgr: cm,
			srv:     srv,
			addr:    addr,
			cancel:  cancel,
			devices: testutil.NewTestDevices(t, store),
		},
		alerts: alertStore,
	}
}

// packEvidence compresses an AlertEvidence exactly as the agent's encoder does,
// so the server reads the blob through its own decoder rather than through one
// written to match this test.
func packEvidence(t *testing.T, evidence protocol.AlertEvidence) []byte {
	t.Helper()

	packed, err := msgpack.Marshal(evidence)
	require.NoError(t, err)

	var out bytes.Buffer
	w, err := flate.NewWriter(&out, flate.BestSpeed)
	require.NoError(t, err)
	_, err = w.Write(packed)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	return out.Bytes()
}

// raiseAlert writes one AgentAlert onto an agent's control stream, the way a
// machine that has just decided something is wrong does.
func raiseAlert(t *testing.T, stream *quic.Stream, evidence []byte) (windowStart time.Time) {
	t.Helper()

	severity := protocol.AlertSeverityCritical
	value := 97.5
	backfilled := false
	// A window that has just closed: recent enough to be inside what the server
	// accepts from a live agent, and stamped in whole seconds because that is
	// the resolution the wire carries.
	end := time.Now().UTC().Truncate(time.Second).Add(-time.Minute)
	start := end.Add(-5 * time.Minute)

	codec := &protocol.Codec{}
	payload, err := codec.EncodeControl(&protocol.ControlMessage{
		Type:          protocol.MsgAgentAlert,
		AlertID:       uuid.NewString(),
		RuleID:        triageRule,
		RuleVersion:   1,
		Severity:      &severity,
		Metric:        "cpu.total",
		Value:         &value,
		WindowStartTS: start.Unix(),
		WindowEndTS:   end.Unix(),
		ObservedTS:    end.Unix(),
		Backfilled:    &backfilled,
		EvidenceCodec: protocol.EvidenceCodec,
		Evidence:      evidence,
	})
	require.NoError(t, err)
	require.NoError(t, codec.WriteFrame(stream, protocol.FrameControl, payload))
	return start
}

// TestTriagePathFromAgentAlertToResolution walks one event from the machine that
// raised it to the technician who closed it.
func TestTriagePathFromAgentAlertToResolution(t *testing.T) {
	t.Parallel()

	env := newInvestigationsEnv(t)
	site := testutil.SeedSite(t, context.Background(), env.store)
	stream, deviceID := env.connectAgent(t, site.ID)
	waitForDeviceStatus(t, env.store, deviceID, db.StatusOnline)

	scope, err := settings.NewPostgresReader(env.store.DB()).
		ScopeFor(defaultTenantContext(), deviceID)
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, scope.OrganizationID,
		"a registered machine belongs to a customer; an alert with none is refused")

	evidence := packEvidence(t, protocol.AlertEvidence{
		Ranked: []protocol.RankedDim{{Dim: "cpu.total", Score: 0.94}},
		Series: []protocol.EvidenceSeries{{
			Dim:    "cpu.total",
			Points: []protocol.HistoryPoint{{TS: time.Now().Unix(), Value: 97.5}},
		}},
		Processes: []protocol.ProcessReportEntry{{Rank: 1, Basename: "indexer", PID: 4242, CPU: 88.0}},
	})
	windowStart := raiseAlert(t, stream, evidence)

	// The alert is filed on a slot goroutine, so the assertion is on the row
	// arriving rather than on the write having returned.
	ctx := defaultTenantContext()
	var alertID uuid.UUID
	require.Eventually(t, func() bool {
		id, found, err := env.alerts.AlertByIdentity(ctx, deviceID, triageRule, 1, windowStart)
		if err != nil || !found {
			return false
		}
		alertID = id
		return true
	}, 10*time.Second, 50*time.Millisecond, "the alert a real agent sent must reach the store")

	// A room opened for it, and it is the room the queue would hand a technician.
	page, err := env.alerts.Queue(ctx, alerts.Filter{OrganizationID: scope.OrganizationID, Limit: 10})
	require.NoError(t, err)
	require.Len(t, page.Incidents, 1, "one alert opens one room")
	incident := page.Incidents[0]
	assert.Equal(t, triageRule, incident.RuleID)
	assert.Equal(t, alerts.StatusNew, incident.Status)

	// The evidence is readable from the room, which is the only place it is ever
	// read from — nothing goes back to the machine to ask again.
	blob, blobCodec, err := env.alerts.Evidence(ctx, incident.ID, alertID)
	require.NoError(t, err)
	assert.Equal(t, protocol.EvidenceCodec, blobCodec)
	decoded, err := protocol.DecodeAlertEvidence(blob, blobCodec)
	require.NoError(t, err)
	require.Len(t, decoded.Processes, 1)
	assert.Equal(t, "indexer", decoded.Processes[0].Basename,
		"the evidence read out of the room is the evidence the machine attached")

	// A technician closes it, and has to say why.
	actor := testutil.SeedUser(t, ctx, env.store).ID
	require.Error(t, env.alerts.Transition(ctx, incident.ID, alerts.Change{
		To: alerts.StatusResolved, Actor: actor,
	}), "a resolution with no cause code is refused")

	require.NoError(t, env.alerts.Transition(ctx, incident.ID, alerts.Change{
		To: alerts.StatusResolved, Cause: alerts.CauseFixedByTech, Actor: actor,
	}))

	closed, err := env.alerts.Incident(ctx, incident.ID, scope.OrganizationID)
	require.NoError(t, err)
	assert.Equal(t, alerts.StatusResolved, closed.Status)
	assert.Equal(t, alerts.CauseFixedByTech, closed.CauseCode)
}
