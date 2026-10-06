// Package acceptance drives the assembled product through a technician's HTTP API and a
// machine's control stream.
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

// eventually is the budget of every outcome wait; polling replaces sleeping.
const (
	eventually = 10 * time.Second
	poll       = 25 * time.Millisecond
)

// productSecret signs the harness's operator tokens and meets the assembly's minimum length.
const productSecret = "acceptance-harness-secret-32-byte"

// Product is one whole installation with its own schema, data directory and listeners,
// sharing nothing with other products.
type Product struct {
	t        *testing.T
	assembly *app.Assembly

	// HTTP is the listener a technician uses.
	HTTP *httptest.Server
	// Internal is the cluster-only listener serving metrics and the profiler, which the ingress
	// does not publish.
	Internal *httptest.Server
	// QUICAddr is the address a machine dials.
	QUICAddr string

	// hardware stands in for Intel management hardware, which a test host cannot provide.
	hardware *managedHardware

	// firstCustomerClaimed records whether the installation's own customer has been named.
	firstCustomerClaimed bool

	// readingStore is the numeric store, flushed on demand so written readings are queryable.
	readingStore *telemetry.VMClient
}

// productOptions carries a test's choices about which parts of the product to stand up.
type productOptions struct {
	numericTelemetry bool
	sweeps           *app.BackgroundSchedule
}

// ProductOption narrows or widens what newProduct stands up.
type ProductOption func(*productOptions)

// WithNumericTelemetry gives the product a real metrics store, which costs a container.
func WithNumericTelemetry() ProductOption {
	return func(o *productOptions) { o.numericTelemetry = true }
}

// WithSweeps runs the product's periodic workers on the caller's cadence, which is far
// shorter than the server's minutes-long default.
func WithSweeps(sched app.BackgroundSchedule) ProductOption {
	return func(o *productOptions) { o.sweeps = &sched }
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
		_ = assembly.Agents.ListenAndServe(ctx, "127.0.0.1:0")
	}()
	quicAddr := assembly.Agents.Addr()

	if options.sweeps != nil {
		require.NoError(t, assembly.StartBackgroundWorkers(ctx, *options.sweeps),
			"the product's periodic workers must start")
	}

	httpSrv := httptest.NewServer(assembly.API)
	internalSrv := httptest.NewServer(assembly.Internal.Handler)

	t.Cleanup(func() {
		internalSrv.Close()
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
		Internal:     internalSrv,
		QUICAddr:     quicAddr,
		hardware:     hardware,
		readingStore: readingStore,
	}
}

// publishReadings makes everything written to the numeric store queryable immediately.
func (p *Product) publishReadings() {
	p.t.Helper()
	if p.readingStore == nil {
		return
	}
	require.NoError(p.t, p.readingStore.Flush(context.Background()))
}

// arrangeTenantContext is the database context of the arrangement helpers.
func arrangeTenantContext() context.Context {
	return dbtx.WithDefaultTenant(context.Background(), false)
}

// arrangeCustomer names a customer. The first call renames the default customer that a
// registering machine lands in; later calls create further customers.
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

// deviceRow reads a machine's row straight from the database.
func (p *Product) deviceRow(id uuid.UUID) (*db.Device, error) {
	return p.assembly.Devices.Get(arrangeTenantContext(), id)
}

// managedHardware stands in for Intel management hardware, recording the actions it receives.
type managedHardware struct {
	connected map[uuid.UUID]bool
	actions   []hardwareAction
}

// hardwareAction is one power instruction sent to a machine's management controller.
type hardwareAction struct {
	Device uuid.UUID
	Action int
}

func newManagedHardware() *managedHardware {
	return &managedHardware{connected: map[uuid.UUID]bool{}}
}

// arrangeReachable marks a machine's management controller as connected to the MPS listener.
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
