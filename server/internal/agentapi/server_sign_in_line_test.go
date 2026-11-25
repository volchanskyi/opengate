package agentapi

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/quic-go/quic-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/device"
	appmetrics "github.com/volchanskyi/opengate/server/internal/metrics"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	dbtestutil "github.com/volchanskyi/opengate/server/internal/testutil"
)

// holdingDevices holds every sign-in until released, so the line fills behind the first.
type holdingDevices struct {
	device.Repository
	entered chan struct{}
	release chan struct{}
}

func (h *holdingDevices) SignIn(ctx context.Context, d *device.Device, needsFleetSize func(device.OrganizationID) bool,
	beforeCommit func(device.SignIn),
) (device.SignIn, error) {
	h.entered <- struct{}{}
	<-h.release
	return h.Repository.SignIn(ctx, d, needsFleetSize, beforeCommit)
}

// oneInOneWaiting is the processor share whose line runs one sign-in and, before it has measured a
// pace, holds one more.
const oneInOneWaiting = 1.0 / (signInLineSeconds * signInsPerSecondPerProcessor)

// readControl reads the next control frame the server sends, within the close wait.
func readControl(t *testing.T, stream *quic.Stream) *protocol.ControlMessage {
	t.Helper()
	require.NoError(t, stream.SetReadDeadline(time.Now().Add(signInCloseWait)))
	codec := &protocol.Codec{}
	frameType, payload, err := codec.ReadFrame(stream)
	require.NoError(t, err)
	require.Equal(t, protocol.FrameControl, frameType)
	msg, err := codec.DecodeControl(payload)
	require.NoError(t, err)
	return msg
}

// fullLine is a server whose line runs one sign-in, held in the store, with one machine waiting.
func fullLine(t *testing.T, m *appmetrics.Metrics) (*acceptEnv, *holdingDevices) {
	t.Helper()
	hold := &holdingDevices{entered: make(chan struct{}, 3), release: make(chan struct{})}
	env := newAcceptEnvTuned(t, func(d device.Repository) device.Repository {
		hold.Repository = d
		return hold
	}, func(cfg *AgentServerConfig) {
		cfg.ProcessorShare = oneInOneWaiting
		cfg.Metrics = m
	})

	_, first := env.connect(t, uuid.New())
	register(t, first, "dal-ws-001")
	select {
	case <-hold.entered:
	case <-time.After(signInCloseWait):
		t.Fatal("the first sign-in never reached the store")
	}
	_, second := env.connect(t, uuid.New())
	register(t, second, "dal-ws-002")
	require.Eventually(t, func() bool { return env.srv.line.waiting() == 1 },
		signInCloseWait, 10*time.Millisecond, "the second machine waits its turn")
	return env, hold
}

func TestASignInPastAFullLineIsToldToComeBackLater(t *testing.T) {
	m := appmetrics.NewMetrics(prometheus.NewRegistry())
	env, hold := fullLine(t, m)
	t.Cleanup(func() { close(hold.release) })
	assert.InDelta(t, 1, testutil.ToFloat64(m.SignInLineWaiting), 0)

	thirdID := uuid.New()
	conn, third := env.connect(t, thirdID)
	register(t, third, "dal-ws-003")

	answer := readControl(t, third)
	assert.Equal(t, protocol.MsgComeBackLater, answer.Type)
	assert.LessOrEqual(t, answer.RetryAfter, uint32(comeBackWithinSeconds))
	assert.Equal(t, quic.ApplicationErrorCode(protocol.CloseBye), closedWithin(t, conn, signInCloseWait))
	assert.InDelta(t, 1, testutil.ToFloat64(m.SignInsTurnedAwayTotal), 0)
	assert.Nil(t, env.srv.GetAgent(protocol.DeviceID(thirdID)), "a machine turned away is held nowhere")
}

func TestTheServerSaysMachinesWaitOnlyWhileOneWaitsItsTurn(t *testing.T) {
	env, hold := fullLine(t, nil)
	assert.True(t, env.srv.MachinesWaiting(), "the second machine waits behind the first")

	close(hold.release)

	waitForCount(t, env.srv, 2)
	assert.False(t, env.srv.MachinesWaiting(), "both machines are in")
}

func TestAWaitingMachineSignsInWhenTheOneAheadFinishes(t *testing.T) {
	env, hold := fullLine(t, nil)

	close(hold.release)

	waitForCount(t, env.srv, 2)
	assert.Zero(t, env.srv.line.waiting())
}

func TestASignInTeachesTheNoteItsMachinesCustomer(t *testing.T) {
	env := newAcceptEnv(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), false)
	contoso := dbtestutil.SeedOrganization(t, ctx, env.store, "Contoso-"+uuid.New().String()[:8])
	machine := dbtestutil.SeedDeviceIn(t, ctx, env.store, contoso, uuid.Nil)
	assert.Equal(t, uuid.Nil, env.srv.customerNote.customerOf(machine.ID), "nothing is known before")

	_, stream := env.connect(t, machine.ID)
	register(t, stream, "dal-ws-004")
	awaitSignedIn(t, stream)

	assert.Equal(t, contoso, env.srv.customerNote.customerOf(machine.ID))
}

func TestTheNoteKnowsEveryTenantsMachinesFromTheStart(t *testing.T) {
	store := dbtestutil.NewTestStore(t)
	devices := dbtestutil.NewTestDevices(t, store)
	var listed directory
	var machines []*device.Device
	for _, name := range []string{"Contoso", "Northwind"} {
		tenant := uuid.New()
		dbtestutil.EnsureTenant(t, context.Background(), store, tenant, name+"-"+tenant.String()[:8])
		ctx := dbtx.WithTenant(context.Background(), tenant, false)
		customer := dbtestutil.SeedOrganization(t, ctx, store, name)
		machine := dbtestutil.SeedDeviceIn(t, ctx, store, customer, uuid.Nil)
		listed = append(listed, CustomerRef{TenantID: tenant, OrganizationID: customer})
		machines = append(machines, machine)
	}
	srv := NewAgentServer(AgentServerConfig{Devices: devices, Customers: listed, Logger: testLogger()})

	require.NoError(t, srv.WarmCustomerNote(context.Background()))

	for _, machine := range machines {
		assert.Equal(t, machine.OrganizationID, srv.customerNote.customerOf(machine.ID))
	}
}

func TestAnErasedMachineLeavesTheNote(t *testing.T) {
	srv := NewAgentServer(AgentServerConfig{Logger: testLogger()})
	machine, contoso := uuid.New(), uuid.New()
	srv.customerNote.remember(machine, contoso)

	srv.DeregisterAgent(context.Background(), machine)

	assert.Equal(t, uuid.Nil, srv.customerNote.customerOf(machine))
}

func TestANoteThatCannotBeReadSaysSo(t *testing.T) {
	srv := NewAgentServer(AgentServerConfig{
		Devices:   &unreachableDevices{},
		Customers: directory{{TenantID: uuid.New(), OrganizationID: uuid.New()}},
		Logger:    testLogger(),
	})

	assert.Error(t, srv.WarmCustomerNote(context.Background()))
}
