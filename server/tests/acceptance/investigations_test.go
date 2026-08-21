package acceptance

import (
	"bytes"
	"compress/flate"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/msgpack/v5"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// triageRule is a rule the shipped catalogue carries, so an alert naming it is
// one the product recognises.
const triageRule = "cpu-saturated"

// incident is one room a customer's alerts are investigated in, as the triage
// queue hands it to a technician.
type incident struct {
	ID        uuid.UUID `json:"id"`
	RuleID    string    `json:"rule_id"`
	Status    string    `json:"status"`
	Severity  string    `json:"severity"`
	CauseCode string    `json:"cause_code"`
}

// triageQueue is the list of rooms waiting for somebody.
func (a *Technician) triageQueue() []incident {
	a.t.Helper()
	var page struct {
		Items []incident `json:"items"`
	}
	reply := a.Get(a.InCustomer("/api/v1/investigations"))
	require.Equalf(a.t, http.StatusOK, reply.Status, "reading the triage queue failed: %s", reply.Text())
	reply.Into(&page)
	return page.Items
}

// raiseAlert is a machine deciding something is wrong and saying so, with the
// evidence it gathered attached — packed by the same encoder the agent uses,
// so the product reads it through its own decoder.
func (m *Machine) raiseAlert(topProcess string) (windowStart time.Time) {
	m.t.Helper()

	packed, err := msgpack.Marshal(protocol.AlertEvidence{
		Ranked: []protocol.RankedDim{{Dim: "cpu.total", Score: 0.94}},
		Series: []protocol.EvidenceSeries{{
			Dim:    "cpu.total",
			Points: []protocol.HistoryPoint{{TS: time.Now().Unix(), Value: 97.5}},
		}},
		Processes: []protocol.ProcessReportEntry{{Rank: 1, Basename: topProcess, PID: 4242, CPU: 88.0}},
	})
	require.NoError(m.t, err)

	var compressed bytes.Buffer
	writer, err := flate.NewWriter(&compressed, flate.BestSpeed)
	require.NoError(m.t, err)
	_, err = writer.Write(packed)
	require.NoError(m.t, err)
	require.NoError(m.t, writer.Close())

	severity := protocol.AlertSeverityCritical
	value := 97.5
	backfilled := false
	// A window that has just closed: recent enough for a live machine, and
	// stamped in whole seconds because that is what the wire carries.
	end := time.Now().UTC().Truncate(time.Second).Add(-time.Minute)
	start := end.Add(-5 * time.Minute)

	m.Send(&protocol.ControlMessage{
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
		Evidence:      compressed.Bytes(),
	})
	return start
}

// awaitIncident waits for the room a machine's alert opened.
func (a *Technician) awaitIncident() incident {
	a.t.Helper()
	var room incident
	require.Eventually(a.t, func() bool {
		queue := a.triageQueue()
		if len(queue) == 0 {
			return false
		}
		room = queue[0]
		return true
	}, eventually, poll, "an alert a machine raised must open a room in the triage queue")
	return room
}

// TestAnAlertBecomesAnIncidentATechnicianClosesWithACause walks one event from
// the machine that raised it to the technician who closed it, and every step of
// the technician's half goes through the API the browser uses.
//
// The repository's one deliberately joined test stopped short of that: its
// technician half called the store directly, because no harness had both a
// machine-facing listener and a wired API. If the API refused the resolution on
// an authorisation or tenancy ground, that test would still have passed.
func TestAnAlertBecomesAnIncidentATechnicianClosesWithACause(t *testing.T) {
	t.Parallel()

	product := newProduct(t)
	contoso := product.arrangeCustomer("Contoso")
	admin := product.Administrator(contoso)

	machine := product.Machine(admin.mintEnrolmentToken("Head Office").Token, "contoso-build-agent",
		protocol.CapTerminal, protocol.CapThresholdAlerts)
	machine.AwaitOnline()

	machine.raiseAlert("indexer")
	room := admin.awaitIncident()
	assert.Equal(t, triageRule, room.RuleID)
	assert.Equal(t, "new", room.Status)

	// The evidence is readable from the room, which is the only place it is
	// ever read from — nothing goes back to the machine to ask again.
	var listed struct {
		Alerts []struct {
			ID uuid.UUID `json:"id"`
		} `json:"alerts"`
	}
	admin.Get(admin.InCustomer("/api/v1/investigations/" + room.ID.String())).Into(&listed)
	require.NotEmpty(t, listed.Alerts, "the room lists the alert that opened it")

	evidence := admin.Get(admin.InCustomer(
		"/api/v1/investigations/" + room.ID.String() + "/alerts/" + listed.Alerts[0].ID.String() + "/evidence"))
	require.Equal(t, http.StatusOK, evidence.Status)
	assert.Contains(t, evidence.Text(), "indexer",
		"what the technician reads is what the machine attached")

	// A technician takes it and closes it, and has to say why.
	require.Equal(t, http.StatusOK, admin.Post(
		admin.InCustomer("/api/v1/investigations/"+room.ID.String()+"/status"),
		map[string]any{"status": "acknowledged"}).Status)

	refused := admin.Post(admin.InCustomer("/api/v1/investigations/"+room.ID.String()+"/status"),
		map[string]any{"status": "resolved"})
	assert.Equal(t, http.StatusBadRequest, refused.Status,
		"a resolution with no cause code spends the feedback the rule pack is tuned from")

	closed := admin.Post(admin.InCustomer("/api/v1/investigations/"+room.ID.String()+"/status"),
		map[string]any{"status": "resolved", "cause_code": "fixed_by_tech"})
	require.Equalf(t, http.StatusOK, closed.Status, "closing the room failed: %s", closed.Text())

	var settled incident
	closed.Into(&settled)
	assert.Equal(t, "resolved", settled.Status)
	assert.Equal(t, "fixed_by_tech", settled.CauseCode)
}

// TestAnIncidentIdFromAnotherTenantIsIndistinguishableFromAMissingOne is the
// technician who guesses. A different status code for "exists but is not
// yours" tells them the room is there, which is the whole of the leak.
func TestAnIncidentIdFromAnotherTenantIsIndistinguishableFromAMissingOne(t *testing.T) {
	t.Parallel()

	product := newProduct(t)
	contoso := product.arrangeCustomer("Contoso")
	admin := product.Administrator(contoso)

	machine := product.Machine(admin.mintEnrolmentToken("Head Office").Token, "contoso-build-agent",
		protocol.CapTerminal, protocol.CapThresholdAlerts)
	machine.AwaitOnline()
	machine.raiseAlert("indexer")
	room := admin.awaitIncident()

	outsider := product.TechnicianIn(product.arrangeSeparateTenant("Northwind"))
	real := outsider.Get("/api/v1/investigations/" + room.ID.String())
	invented := outsider.Get("/api/v1/investigations/" + uuid.NewString())

	assert.Equal(t, invented.Status, real.Status,
		"a room that exists and a room that does not must answer the same to somebody who may see neither")
}
