package agentapi

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// watchingDevices notes, as each sign-in's write commits, whether the server already holds the
// machine's connection.
type watchingDevices struct {
	device.Repository
	srv  *AgentServer
	held chan bool
}

func (w *watchingDevices) SignIn(ctx context.Context, d *device.Device, needsFleetSize func(device.OrganizationID) bool,
	beforeCommit func(device.SignIn),
) (device.SignIn, error) {
	got, err := w.Repository.SignIn(ctx, d, needsFleetSize, beforeCommit)
	w.held <- w.srv.GetAgent(protocol.DeviceID(d.ID)) != nil
	return got, err
}

// errCommitLost is a store losing a sign-in's commit after its statements ran.
var errCommitLost = errors.New("commit lost")

// losingDevices commits its first kept sign-ins, then runs each one after up to its commit and
// loses the commit.
type losingDevices struct {
	device.Repository
	kept atomic.Int32
	lost chan struct{}
}

func (l *losingDevices) SignIn(ctx context.Context, d *device.Device, needsFleetSize func(device.OrganizationID) bool,
	beforeCommit func(device.SignIn),
) (device.SignIn, error) {
	if l.kept.Add(-1) >= 0 {
		return l.Repository.SignIn(ctx, d, needsFleetSize, beforeCommit)
	}
	beforeCommit(device.SignIn{TenantID: uuid.New()})
	defer close(l.lost)
	return device.SignIn{}, errCommitLost
}

// awaitLost waits for the store to lose a sign-in's commit.
func (l *losingDevices) awaitLost(t *testing.T) {
	t.Helper()
	select {
	case <-l.lost:
	case <-time.After(signInCloseWait):
		t.Fatal("the sign-in never reached the store")
	}
}

// newLosingEnv serves machines over a store that commits kept sign-ins and loses the next.
func newLosingEnv(t *testing.T, kept int32) (*acceptEnv, *losingDevices) {
	t.Helper()
	losing := &losingDevices{lost: make(chan struct{})}
	losing.kept.Store(kept)
	env := newAcceptEnvOver(t, nil, func(d device.Repository) device.Repository {
		losing.Repository = d
		return losing
	})
	return env, losing
}

func TestAMachineIsReachableTheMomentItsOnlineStatusCommits(t *testing.T) {
	watch := &watchingDevices{held: make(chan bool, 1)}
	env := newAcceptEnvOver(t, nil, func(d device.Repository) device.Repository {
		watch.Repository = d
		return watch
	})
	watch.srv = env.srv

	_, stream := env.connect(t, uuid.New())
	register(t, stream, "contoso-desk-01")

	select {
	case held := <-watch.held:
		assert.True(t, held, "a push sent when the console first shows the machine online reaches it")
	case <-time.After(signInCloseWait):
		t.Fatal("the sign-in never reached the store")
	}
}

func TestASignInWhoseCommitIsLostLeavesNoConnectionHeld(t *testing.T) {
	env, losing := newLosingEnv(t, 0)
	machine := uuid.New()

	conn, stream := env.connect(t, machine)
	register(t, stream, "contoso-desk-02")

	losing.awaitLost(t)
	closedWithin(t, conn, signInCloseWait)
	assert.Nil(t, env.srv.GetAgent(protocol.DeviceID(machine)), "a machine whose sign-in was lost is held nowhere")
	assert.Zero(t, env.srv.ConnectedAgentCount())
}

func TestASignInWhoseCommitIsLostLeavesTheConnectionItWouldReplace(t *testing.T) {
	env, losing := newLosingEnv(t, 1)
	machine := uuid.New()
	identity := env.machineTLS(t, machine)
	first, firstStream := env.dialWith(t, identity)
	readServerHello(t, firstStream)
	register(t, firstStream, "contoso-desk-03")
	awaitSignedIn(t, firstStream)

	second, secondStream := env.dialWith(t, identity)
	readServerHello(t, secondStream)
	register(t, secondStream, "contoso-desk-03")

	losing.awaitLost(t)
	closedWithin(t, second, signInCloseWait)
	assert.NoError(t, first.Context().Err(), "the connection that signed in stays open")
	assert.NotNil(t, env.srv.GetAgent(protocol.DeviceID(machine)), "the machine is still reachable")
	assert.Equal(t, 1, env.srv.ConnectedAgentCount())
}
