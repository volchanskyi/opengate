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

// triageRule names a rule in the shipped catalogue, which the product recognises.
const triageRule = "cpu-saturated"

// incident is one investigation room as the triage queue lists it.
type incident struct {
	ID        uuid.UUID `json:"id"`
	RuleID    string    `json:"rule_id"`
	Status    string    `json:"status"`
	Severity  string    `json:"severity"`
	CauseCode string    `json:"cause_code"`
	Scope     string    `json:"scope"`
	ScopeName *string   `json:"scope_name"`
}

// triageQueue lists the rooms awaiting a technician.
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

// raiseAlert sends an alert with msgpack-packed, deflate-compressed evidence attached.
func (m *Machine) raiseAlert(topProcess string) (windowStart time.Time) {
	m.t.Helper()

	share := 37.5
	packed, err := msgpack.Marshal(protocol.AlertEvidence{
		Ranked: []protocol.RankedDim{{Dim: "cpu.total", Score: 0.94}},
		Series: []protocol.EvidenceSeries{{
			Dim:    "cpu.total",
			Points: []protocol.HistoryPoint{{TS: time.Now().Unix(), Value: 97.5}},
		}},
		Processes: []protocol.EvidenceProcess{{Rank: 1, Basename: topProcess, PID: 4242, CPUShare: &share}},
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
	// The wire carries whole seconds, so the window end is truncated to one.
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
	existing := outsider.Get("/api/v1/investigations/" + room.ID.String())
	invented := outsider.Get("/api/v1/investigations/" + uuid.NewString())

	assert.Equal(t, invented.Status, existing.Status,
		"a room that exists and a room that does not must answer the same to somebody who may see neither")
}

// foldedAlert is one alert as its room lists it.
type foldedAlert struct {
	ID         uuid.UUID `json:"id"`
	RuleID     string    `json:"rule_id"`
	Severity   string    `json:"severity"`
	Backfilled bool      `json:"backfilled"`
	ObservedAt time.Time `json:"observed_at"`
}

// investigation is a room with the alerts folded into it.
type investigation struct {
	Incident incident      `json:"incident"`
	Alerts   []foldedAlert `json:"alerts"`
}

// openIncident reads one room through the browser's API.
func (a *Technician) openIncident(id uuid.UUID) investigation {
	a.t.Helper()
	var room investigation
	reply := a.Get(a.InCustomer("/api/v1/investigations/" + id.String()))
	require.Equalf(a.t, http.StatusOK, reply.Status, "opening the room failed: %s", reply.Text())
	reply.Into(&room)
	return room
}

// raiseWordAlert reports a log finding: it names no reading, carries the redacted record as
// evidence, and uses the record's instant as both ends of the window.
func (m *Machine) raiseWordAlert(ruleID string) {
	m.t.Helper()

	packed, err := msgpack.Marshal(protocol.AlertEvidence{
		LogSamples: []string{"Out of memory: Killed process 4242 (reporting-svc)"},
	})
	require.NoError(m.t, err)
	var compressed bytes.Buffer
	writer, err := flate.NewWriter(&compressed, flate.BestSpeed)
	require.NoError(m.t, err)
	_, err = writer.Write(packed)
	require.NoError(m.t, err)
	require.NoError(m.t, writer.Close())

	severity := protocol.AlertSeverityCritical
	backfilled := false
	at := time.Now().UTC().Truncate(time.Second).Add(-time.Minute)

	m.Send(&protocol.ControlMessage{
		Type:          protocol.MsgAgentAlert,
		AlertID:       uuid.NewString(),
		RuleID:        ruleID,
		RuleVersion:   1,
		Severity:      &severity,
		WindowStartTS: at.Unix(),
		WindowEndTS:   at.Unix(),
		ObservedTS:    at.Unix(),
		Backfilled:    &backfilled,
		EvidenceCodec: protocol.EvidenceCodec,
		Evidence:      compressed.Bytes(),
	})
}

// raiseFinding reports what a new rule caught in history, stamped with the second the event
// happened.
func (m *Machine) raiseFinding(ruleID string, happenedAt time.Time) {
	m.t.Helper()

	severity := protocol.AlertSeverityWarning
	backfilled := true
	value := 97.5
	at := happenedAt.UTC().Truncate(time.Second)

	m.Send(&protocol.ControlMessage{
		Type:          protocol.MsgAgentAlert,
		AlertID:       uuid.NewString(),
		RuleID:        ruleID,
		RuleVersion:   1,
		Severity:      &severity,
		Metric:        "cpu.total",
		Value:         &value,
		WindowStartTS: at.Add(-5 * time.Minute).Unix(),
		WindowEndTS:   at.Unix(),
		ObservedTS:    at.Unix(),
		Backfilled:    &backfilled,
	})
}

// namedRoom is a room as its reader sees it: what it is about, and who is in it.
type namedRoom struct {
	Incident incident `json:"incident"`
	Alerts   []struct {
		Hostname *string `json:"hostname"`
	} `json:"alerts"`
	People map[uuid.UUID]string `json:"people"`
}

func (a *Technician) readNamedRoom(id uuid.UUID) namedRoom {
	a.t.Helper()
	var room namedRoom
	reply := a.Get(a.InCustomer("/api/v1/investigations/" + id.String()))
	require.Equalf(a.t, http.StatusOK, reply.Status, "opening the room failed: %s", reply.Text())
	reply.Into(&room)
	return room
}

func TestAnIncidentNamesItsHostAndThePeopleWorkingIt(t *testing.T) {
	t.Parallel()

	// Erasing a machine runs the purge, which the product wires beside its numeric store.
	product := newProduct(t, WithNumericTelemetry())
	contoso := product.arrangeCustomer("Contoso")
	admin := product.Administrator(contoso)

	machine := product.Machine(admin.mintEnrolmentToken("Head Office").Token, "contoso-build-agent",
		protocol.CapTerminal, protocol.CapThresholdAlerts)
	machine.AwaitOnline()
	machine.raiseAlert("indexer")
	room := admin.awaitIncident()

	assert.Equal(t, "device", room.Scope)
	require.NotNil(t, room.ScopeName, "the queue names the host, never only its id")
	assert.Equal(t, "contoso-build-agent", *room.ScopeName)

	assignee := admin.InCustomer("/api/v1/investigations/" + room.ID.String() + "/assignee")
	require.Equal(t, http.StatusOK, admin.Post(assignee, map[string]any{"assignee_id": admin.User.ID}).Status)

	outsider := product.TechnicianIn(product.arrangeSeparateTenant("Northwind"))
	assert.Equal(t, http.StatusNotFound,
		admin.Post(assignee, map[string]any{"assignee_id": outsider.User.ID}).Status,
		"a user of another tenant cannot be handed the room")

	opened := admin.readNamedRoom(room.ID)
	require.NotNil(t, opened.Incident.ScopeName)
	assert.Equal(t, "contoso-build-agent", *opened.Incident.ScopeName)
	require.NotEmpty(t, opened.Alerts)
	require.NotNil(t, opened.Alerts[0].Hostname)
	assert.Equal(t, "contoso-build-agent", *opened.Alerts[0].Hostname)
	assert.Equal(t, map[uuid.UUID]string{admin.User.ID: admin.User.DisplayName}, opened.People,
		"the room names the people in it, and nobody outside the tenant")

	require.Equal(t, http.StatusNoContent, admin.Delete("/api/v1/devices/"+machine.DeviceID.String()).Status)
	require.Eventually(t, func() bool {
		return admin.readNamedRoom(room.ID).Incident.ScopeName == nil
	}, eventually, poll, "a room about a removed host names nothing rather than a stale id")
}
