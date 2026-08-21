// Package acceptance states what the product does, in the words a customer
// would use, against the product as it is actually assembled.
//
// A test here stands the whole thing up — the composition root in
// internal/app, a real database, an HTTP listener and a QUIC listener — and
// then speaks through exactly two doors, because a real installation has
// exactly two: a technician at the HTTP API, and a machine on the control
// stream. Reaching past them into a repository is not an acceptance test. The
// single exception is arranging a precondition the product offers no door for,
// and every helper that does so is named `arrange…` so the exception is
// visible in the test's own text.
package acceptance

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/amt"
	"github.com/volchanskyi/opengate/server/internal/amt/transport/wsman"
	"github.com/volchanskyi/opengate/server/internal/app"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/telemetry"
	"github.com/volchanskyi/opengate/server/internal/testutil"
	"github.com/volchanskyi/opengate/server/internal/testvm"
)

// eventually is the synchronisation budget every wait in this package uses.
// Nothing here sleeps: a test waits on an outcome an actor can observe, or it
// does not wait at all.
const (
	eventually = 10 * time.Second
	poll       = 25 * time.Millisecond
)

// productSecret signs this harness's operator tokens. It is a fixed value
// because the assembly refuses anything shorter than the shipped minimum, and
// a per-test secret would prove nothing extra.
const productSecret = "acceptance-harness-secret-32-byte"

// Product is one whole installation: its own database schema, its own data
// directory, its own listeners. Products share nothing, so every test in this
// package runs in parallel.
type Product struct {
	t        *testing.T
	assembly *app.Assembly

	// HTTP is the door a technician knocks on.
	HTTP *httptest.Server
	// QUICAddr is the door a machine dials.
	QUICAddr string

	// hardware is the stand-in for Intel management hardware, which answers on
	// its own network path and cannot be stood up by a test host.
	hardware *managedHardware

	// firstCustomerClaimed records whether the installation's own customer has
	// been named yet. See arrangeCustomer.
	firstCustomerClaimed bool

	// readingStore is the numeric metrics store, when the product was given
	// one. Held so a test can make a just-written reading queryable instead of
	// waiting out the store's own flush timer.
	readingStore *telemetry.VMClient
}

// productOptions carries the choices a test makes about how much of the
// product it needs standing.
type productOptions struct {
	numericTelemetry bool
}

// ProductOption narrows or widens what newProduct stands up.
type ProductOption func(*productOptions)

// WithNumericTelemetry gives the product a real metrics store, which is what
// the readings round-trip and series erasure need. It costs a container, so it
// is opt-in rather than the default.
func WithNumericTelemetry() ProductOption {
	return func(o *productOptions) { o.numericTelemetry = true }
}

// newProduct stands the product up and returns it ready to be spoken to.
func newProduct(t *testing.T, opts ...ProductOption) *Product {
	t.Helper()

	var options productOptions
	for _, opt := range opts {
		opt(&options)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	hardware := newManagedHardware()

	cfg := app.Config{
		Store:       testutil.NewTestStore(t),
		DataDir:     t.TempDir(),
		JWTSecret:   productSecret,
		Logger:      logger,
		AMTOperator: hardware,
	}
	var readingStore *telemetry.VMClient
	if options.numericTelemetry {
		cfg.VictoriaMetricsURL = testvm.BaseURL(t)
		readingStore = telemetry.NewVMClient(cfg.VictoriaMetricsURL, nil)
	}

	ctx, cancel := context.WithCancel(context.Background())
	assembly, err := app.Build(ctx, cfg)
	require.NoError(t, err, "the product must assemble; a missing port fails here, naming itself")

	listening := make(chan struct{})
	go func() {
		defer close(listening)
		// The listener stops when ctx is cancelled, which is the cleanup path.
		_ = assembly.Agents.ListenAndServe(ctx, "127.0.0.1:0")
	}()
	quicAddr := assembly.Agents.Addr()

	httpSrv := httptest.NewServer(assembly.API)

	t.Cleanup(func() {
		httpSrv.Close()
		cancel()
		select {
		case <-listening:
		case <-time.After(2 * time.Second):
			t.Log("the machine-facing listener did not stop within 2s")
		}
	})

	return &Product{
		t:            t,
		assembly:     assembly,
		HTTP:         httpSrv,
		QUICAddr:     quicAddr,
		hardware:     hardware,
		readingStore: readingStore,
	}
}

// publishReadings makes everything already written to the numeric store
// queryable now, instead of when its own flush timer next fires. Waiting that
// timer out costs seconds of wall clock per test and buys nothing: the
// question an outcome asks is whether the product wrote the reading, not how
// long its store batches for.
func (p *Product) publishReadings() {
	p.t.Helper()
	if p.readingStore == nil {
		return
	}
	require.NoError(p.t, p.readingStore.Flush(context.Background()))
}

// arrangeTenantContext is the database context the arrangement helpers use.
// Nothing an actor does goes through it.
func arrangeTenantContext() context.Context {
	return dbtx.WithDefaultTenant(context.Background(), false)
}

// arrangeCustomer names a customer. A fresh installation already has exactly
// one — it is created with the schema so a machine always has somewhere to
// belong — and that is the one a registering machine lands in, so the first
// customer a test names claims it rather than creating a rival beside it.
// Naming a second customer creates one, the way an operator adds a customer.
func (p *Product) arrangeCustomer(name string) uuid.UUID {
	p.t.Helper()
	ctx := arrangeTenantContext()

	if p.firstCustomerClaimed {
		return testutil.SeedOrganization(p.t, ctx, p.assembly.Store, name)
	}
	p.firstCustomerClaimed = true

	existing, err := p.assembly.Organizations.EnsureDefault(ctx)
	require.NoError(p.t, err)
	require.NoError(p.t, p.assembly.Organizations.Rename(ctx, existing, name))
	return existing
}

// deviceRow reads a machine's row straight from the database. It exists for
// the two outcomes whose subject is the row itself — a machine appearing, and
// a machine being erased — and for nothing else.
func (p *Product) deviceRow(id uuid.UUID) (*db.Device, error) {
	return p.assembly.Devices.Get(arrangeTenantContext(), id)
}

// managedHardware stands in for Intel management hardware. The real thing
// answers over a side-band network path a test host has no way to provide, so
// this records what it was asked to do and answers the way hardware in that
// state would.
type managedHardware struct {
	connected map[uuid.UUID]bool
	actions   []hardwareAction
}

// hardwareAction is one power instruction the product sent to a machine's
// management controller.
type hardwareAction struct {
	Device uuid.UUID
	Action int
}

func newManagedHardware() *managedHardware {
	return &managedHardware{connected: map[uuid.UUID]bool{}}
}

// arrangeReachable marks a machine's management controller as calling in, the
// state it reaches by dialling the MPS listener from the customer's network.
func (h *managedHardware) arrangeReachable(id uuid.UUID) { h.connected[id] = true }

func (h *managedHardware) PowerAction(_ context.Context, id uuid.UUID, action int) error {
	if !h.connected[id] {
		return amt.ErrDeviceNotConnected
	}
	h.actions = append(h.actions, hardwareAction{Device: id, Action: action})
	return nil
}

func (h *managedHardware) QueryDeviceInfo(_ context.Context, id uuid.UUID) (*wsman.DeviceInfo, error) {
	if !h.connected[id] {
		return nil, amt.ErrDeviceNotConnected
	}
	return &wsman.DeviceInfo{}, nil
}

func (h *managedHardware) ConnectedDeviceCount() int { return len(h.connected) }
