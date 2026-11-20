package agentapi

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/inventory"
	appmetrics "github.com/volchanskyi/opengate/server/internal/metrics"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// answeringInventory answers every write with err and reports each answer.
type answeringInventory struct {
	err      error
	answered chan struct{}
}

func (r *answeringInventory) Replace(context.Context, uuid.UUID, time.Time, []inventory.Component) error {
	defer func() { r.answered <- struct{}{} }()
	return r.err
}

func (r *answeringInventory) ListForDevice(context.Context, uuid.UUID, int) ([]inventory.Component, error) {
	return nil, nil
}

// reportInventory hands the connection one package inventory and waits for its write's answer.
func reportInventory(t *testing.T, inv *answeringInventory) *appmetrics.Metrics {
	t.Helper()
	tenant := uuid.New()
	ac, _ := discoveryConn(t, tenant, inv)
	ac.metrics = appmetrics.NewMetrics(prometheus.NewRegistry())
	msg := &protocol.ControlMessage{
		Type: protocol.MsgDiscoveryReport, TS: time.Now().Unix(),
		Packages: []protocol.DiscoveredPackage{{Name: "openssl", Version: "3.0.13"}},
	}
	require.NoError(t, ac.handleDiscoveryReport(tenantCtx(tenant), msg, 512))
	select {
	case <-inv.answered:
	case <-time.After(2 * time.Second):
		t.Fatal("the inventory was never written")
	}
	require.Eventually(t, func() bool { return len(ac.telemetrySlots) == 0 }, 2*time.Second, time.Millisecond,
		"the write's slot comes back once its answer is counted")
	return ac.metrics
}

func TestAStoredInventoryIsCounted(t *testing.T) {
	m := reportInventory(t, &answeringInventory{answered: make(chan struct{}, 1)})

	assert.InDelta(t, 1, testutil.ToFloat64(m.InventoryReportsStoredTotal), 0)
}

func TestAnInventoryTheDatabaseRefusedIsNotCountedStored(t *testing.T) {
	m := reportInventory(t, &answeringInventory{err: errors.New("database gone"), answered: make(chan struct{}, 1)})

	assert.Zero(t, testutil.ToFloat64(m.InventoryReportsStoredTotal))
}
