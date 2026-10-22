package acceptance

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
	"github.com/pion/stun/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/app"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

func contosoOnlineMachine(t *testing.T, product *Product, hostname string) (*Technician, *Machine) {
	t.Helper()
	contoso := product.arrangeCustomer("Contoso")
	admin := product.Administrator(contoso)
	machine := product.Machine(admin.mintEnrolmentToken("Head Office").Token, hostname)
	machine.AwaitOnline()
	return admin, machine
}

func (a *Technician) openSession(deviceID uuid.UUID) Reply {
	a.t.Helper()
	return a.Post("/api/v1/sessions", map[string]any{"device_id": deviceID.String()})
}

func requireSessionsRefused(t *testing.T, admin *Technician, deviceID uuid.UUID, msg string) {
	t.Helper()
	require.Eventually(t, func() bool {
		return admin.openSession(deviceID).Status == http.StatusConflict
	}, eventually, poll, msg)
}

func TestATechnicianOpensATerminalAndTheMachineIsToldToStartIt(t *testing.T) {
	t.Parallel()

	admin, machine := contosoOnlineMachine(t, newProduct(t), "contoso-desk-01")

	var session struct {
		Token    string `json:"token"`
		RelayURL string `json:"relay_url"`
	}
	reply := admin.openSession(machine.DeviceID)
	require.Equalf(t, http.StatusCreated, reply.Status, "opening a terminal failed: %s", reply.Text())
	reply.Into(&session)
	require.NotEmpty(t, session.Token)
	assert.NotEmpty(t, session.RelayURL, "the browser is told where to connect")

	request := machine.Await(protocol.MsgSessionRequest)
	assert.Equal(t, session.Token, string(request.Token),
		"the machine is asked to start the very session the technician was given")
}

func TestASessionForAMachineThatIsOfflineIsRefusedWithAReason(t *testing.T) {
	t.Parallel()

	admin, machine := contosoOnlineMachine(t, newProduct(t), "contoso-desk-02")
	machine.Disconnect()

	requireSessionsRefused(t, admin, machine.DeviceID, "a terminal on a machine that is not there is refused")

	// Either refusal wording is valid, depending on whether the departure was noticed first.
	reply := admin.openSession(machine.DeviceID)
	assert.Equal(t, http.StatusConflict, reply.Status)
	assert.Containsf(t, reply.Text(), "agent",
		"the refusal names the reason a technician can act on, got %s", reply.Text())
}

func TestASessionOnAMachineThatDisappearsStopsBeingUsable(t *testing.T) {
	t.Parallel()

	admin, machine := contosoOnlineMachine(t, newProduct(t), "contoso-desk-03")

	var session struct {
		Token string `json:"token"`
	}
	reply := admin.openSession(machine.DeviceID)
	require.Equal(t, http.StatusCreated, reply.Status)
	reply.Into(&session)
	machine.Await(protocol.MsgSessionRequest)

	machine.Disconnect()

	requireSessionsRefused(t, admin, machine.DeviceID, "a machine that has gone can carry no further work")

	// A reopen the refusal loop sent while the machine still looked present replaced the session
	// and then failed to reach the machine, which clears it; otherwise the technician clears it.
	cleared := admin.Delete("/api/v1/devices/" + machine.DeviceID.String() + "/session").Status
	assert.Contains(t, []int{http.StatusNoContent, http.StatusNotFound}, cleared)
	assert.Empty(t, admin.sessionsOn(machine.DeviceID),
		"nothing is left holding the machine, so it opens at once when it returns")
}

func TestASessionLeftByAMachineThatWentAwayIsReclaimed(t *testing.T) {
	t.Parallel()

	product := newProduct(t, WithSweeps(app.BackgroundSchedule{
		Gauges:         time.Second,
		DBSize:         time.Second,
		Investigations: time.Second,
		Reconcile:      time.Hour,
		SessionSweep:   50 * time.Millisecond,
		SessionGrace:   time.Nanosecond,
		IncidentSweep:  time.Hour,
		// The retention horizon exceeds anything this run creates, leaving the age sweep idle.
		RetentionSweep:   time.Hour,
		RetentionHorizon: 365 * 24 * time.Hour,
	}))
	admin, machine := contosoOnlineMachine(t, product, "contoso-desk-05")

	reply := admin.openSession(machine.DeviceID)
	require.Equal(t, http.StatusCreated, reply.Status)
	machine.Await(protocol.MsgSessionRequest)

	machine.Disconnect()

	require.Eventually(t, func() bool {
		return len(admin.sessionsOn(machine.DeviceID)) == 0
	}, eventually, poll, "a session nobody is holding stops being one the machine has")
}

// sessionsOn lists the sessions of one machine.
func (a *Technician) sessionsOn(deviceID uuid.UUID) []sessionSummary {
	a.t.Helper()
	var listed []sessionSummary
	reply := a.Get("/api/v1/sessions?device_id=" + deviceID.String())
	require.Equalf(a.t, http.StatusOK, reply.Status, "reading the session list failed: %s", reply.Text())
	reply.Into(&listed)
	return listed
}

// sessionSummary is a session as the session list shows it.
type sessionSummary struct {
	UserID     uuid.UUID `json:"user_id"`
	HolderName string    `json:"holder_name"`
	CreatedAt  time.Time `json:"created_at"`
}

// openedSession is what a technician is handed when a session opens.
type openedSession struct {
	Token      string `json:"token"`
	RelayURL   string `json:"relay_url"`
	IceServers []struct {
		URLs []string `json:"urls"`
	} `json:"ice_servers"`
}

// sessionRefusal is the reason a session was not opened.
type sessionRefusal struct {
	Error      string    `json:"error"`
	HolderName string    `json:"holder_name"`
	StartedAt  time.Time `json:"started_at"`
}

// mustOpenSession opens a session and returns what the technician was handed.
func (a *Technician) mustOpenSession(deviceID uuid.UUID) openedSession {
	a.t.Helper()
	var opened openedSession
	reply := a.openSession(deviceID)
	require.Equalf(a.t, http.StatusCreated, reply.Status, "opening a session failed: %s", reply.Text())
	reply.Into(&opened)
	return opened
}

// joinRelay connects as the browser side of a session, the way the technician's page does.
func (a *Technician) joinRelay(token string) (*websocket.Conn, error) {
	a.t.Helper()
	relay, err := url.Parse(a.product.HTTP.URL)
	require.NoError(a.t, err)
	relay.Scheme = "ws"
	relay.Path = "/ws/relay/" + token
	relay.RawQuery = url.Values{"side": {"browser"}}.Encode()
	conn, _, err := websocket.Dial(a.t.Context(), relay.String(), &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer " + a.token}},
	})
	if err != nil {
		return nil, err
	}
	a.t.Cleanup(func() { _ = conn.CloseNow() })
	return conn, nil
}

// closedWith waits for the relay to close conn and returns the code and reason it gave.
func closedWith(t *testing.T, conn *websocket.Conn) (websocket.StatusCode, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), eventually)
	defer cancel()
	for {
		_, _, err := conn.Read(ctx)
		if err == nil {
			continue
		}
		var closed websocket.CloseError
		require.Truef(t, errors.As(err, &closed), "the relay dropped the connection with no close: %v", err)
		return closed.Code, closed.Reason
	}
}

// sessionEnded is the close code the relay gives a session ended from outside it.
const sessionEnded websocket.StatusCode = 4000

func TestTheMachineIsToldTheConfiguredRelayAddressAndTheTechnicianTheirOwn(t *testing.T) {
	t.Parallel()

	admin, machine := contosoOnlineMachine(t, newProduct(t), "contoso-desk-06")

	var opened openedSession
	reply := admin.doAt("console.contoso.example", http.MethodPost, "/api/v1/sessions",
		map[string]any{"device_id": machine.DeviceID.String()})
	require.Equalf(t, http.StatusCreated, reply.Status, "opening a session failed: %s", reply.Text())
	reply.Into(&opened)

	request := machine.Await(protocol.MsgSessionRequest)
	assert.Equal(t, machinesRelayBase+"/ws/relay/"+opened.Token, request.RelayURL,
		"the machine dials the address the product was configured with, whatever the request said")
	assert.Equal(t, []string{sessionHelper}, request.IceServers)

	assert.Equal(t, "ws://console.contoso.example/ws/relay/"+opened.Token, opened.RelayURL,
		"the technician connects back to the address they reached the product on")
	require.Len(t, opened.IceServers, 1)
	assert.Equal(t, []string{sessionHelper}, opened.IceServers[0].URLs,
		"both sides ask the same helper")
}

func TestOneTechnicianHoldsAHostAndEveryoneElseIsToldWho(t *testing.T) {
	t.Parallel()

	product := newProduct(t)
	_, machine := contosoOnlineMachine(t, product, "contoso-desk-07")
	contoso := product.arrangeCustomer("Contoso Branch")

	const technicians = 20
	type outcome struct {
		status  int
		name    string
		refusal sessionRefusal
	}
	outcomes := make([]outcome, technicians)
	var opening sync.WaitGroup
	start := make(chan struct{})
	for i := range technicians {
		name := "Technician " + string(rune('A'+i))
		tech := product.NamedTechnician(contoso, name)
		opening.Add(1)
		go func() {
			defer opening.Done()
			<-start
			reply := tech.openSession(machine.DeviceID)
			outcomes[i] = outcome{status: reply.Status, name: name}
			if reply.Status == http.StatusConflict {
				reply.Into(&outcomes[i].refusal)
			}
		}()
	}
	close(start)
	opening.Wait()

	var holder string
	opened := 0
	for _, o := range outcomes {
		if o.status == http.StatusCreated {
			opened++
			holder = o.name
		}
	}
	require.Equal(t, 1, opened, "a host takes one session however many technicians click at once")
	for _, o := range outcomes {
		if o.status == http.StatusCreated {
			continue
		}
		require.Equal(t, http.StatusConflict, o.status)
		assert.Equal(t, holder, o.refusal.HolderName, "a refused technician is told who has the host")
		assert.False(t, o.refusal.StartedAt.IsZero(), "and since when")
	}
}

func TestATechnicianReopeningTheirOwnHostReplacesTheirSession(t *testing.T) {
	t.Parallel()

	admin, machine := contosoOnlineMachine(t, newProduct(t), "contoso-desk-08")

	first := admin.mustOpenSession(machine.DeviceID)
	tab, err := admin.joinRelay(first.Token)
	require.NoError(t, err)

	second := admin.mustOpenSession(machine.DeviceID)
	assert.NotEqual(t, first.Token, second.Token)

	code, reason := closedWith(t, tab)
	assert.Equal(t, sessionEnded, code)
	assert.Equal(t, "Opened elsewhere", reason, "the old tab learns the session moved")

	listed := admin.sessionsOn(machine.DeviceID)
	require.Len(t, listed, 1, "the host holds the new session alone")
	assert.Equal(t, admin.User.ID, listed[0].UserID)
}

func TestTheRelayAdmitsOnlyTheHoldersBrowser(t *testing.T) {
	t.Parallel()

	product := newProduct(t)
	alice, machine := contosoOnlineMachine(t, product, "contoso-desk-09")
	bob := product.Technician(alice.Customer)

	opened := alice.mustOpenSession(machine.DeviceID)

	conn, err := bob.joinRelay(opened.Token)
	if err == nil {
		code, _ := closedWith(t, conn)
		assert.Equal(t, websocket.StatusPolicyViolation, code,
			"a member holding someone else's token still attaches nobody")
	}

	_, err = alice.joinRelay(opened.Token)
	require.NoError(t, err, "the holder's own browser is admitted")
}

func TestOnlyTheHolderOrAnAdministratorEndsASession(t *testing.T) {
	t.Parallel()

	product := newProduct(t)
	_, machine := contosoOnlineMachine(t, product, "contoso-desk-10")
	customer := product.arrangeCustomer("Contoso Dental")
	alice := product.NamedTechnician(customer, "Alice")
	bob := product.NamedTechnician(customer, "Bob")
	carol := product.NamedAdministrator(customer, "Carol")

	opened := alice.mustOpenSession(machine.DeviceID)
	tab, err := alice.joinRelay(opened.Token)
	require.NoError(t, err)

	assert.Equal(t, http.StatusForbidden, bob.Delete("/api/v1/sessions/"+opened.Token).Status)
	assert.Equal(t, http.StatusForbidden,
		bob.Delete("/api/v1/devices/"+machine.DeviceID.String()+"/session").Status,
		"a technician cannot end another's session")
	require.Len(t, bob.sessionsOn(machine.DeviceID), 1, "and the session carries on")

	require.Equal(t, http.StatusNoContent,
		carol.Delete("/api/v1/devices/"+machine.DeviceID.String()+"/session").Status,
		"an administrator ends it")
	code, reason := closedWith(t, tab)
	assert.Equal(t, sessionEnded, code)
	assert.Equal(t, "Ended by Carol", reason, "the holder's tab says who ended it")
	assert.Empty(t, bob.sessionsOn(machine.DeviceID))

	bobs := bob.mustOpenSession(machine.DeviceID)
	bobTab, err := bob.joinRelay(bobs.Token)
	require.NoError(t, err)
	require.Equal(t, http.StatusNoContent, bob.Delete("/api/v1/sessions/"+bobs.Token).Status,
		"the holder ends their own session")
	_, reason = closedWith(t, bobTab)
	assert.Equal(t, "Ended by Bob", reason)
}

func TestTheHostAndItsSessionListNameTheHolderAndNeverTheToken(t *testing.T) {
	t.Parallel()

	product := newProduct(t)
	_, machine := contosoOnlineMachine(t, product, "contoso-desk-11")
	customer := product.arrangeCustomer("Contoso Labs")
	alice := product.NamedTechnician(customer, "Alice")
	bob := product.Technician(customer)

	opened := alice.mustOpenSession(machine.DeviceID)

	var host struct {
		Session *struct {
			HolderID   uuid.UUID `json:"holder_id"`
			HolderName string    `json:"holder_name"`
			StartedAt  time.Time `json:"started_at"`
		} `json:"session"`
	}
	bob.Get("/api/v1/devices/" + machine.DeviceID.String()).Into(&host)
	require.NotNil(t, host.Session, "everyone sees that the host is held")
	assert.Equal(t, "Alice", host.Session.HolderName)
	assert.Equal(t, alice.User.ID, host.Session.HolderID)
	assert.WithinDuration(t, time.Now(), host.Session.StartedAt, time.Minute)

	list := bob.Get("/api/v1/sessions?device_id=" + machine.DeviceID.String())
	require.Equal(t, http.StatusOK, list.Status)
	assert.NotContains(t, list.Text(), opened.Token, "no page is handed a session's credential")
	assert.Contains(t, list.Text(), `"holder_name":"Alice"`)

	var fleet []struct {
		ID      uuid.UUID `json:"id"`
		Session *struct {
			HolderName string `json:"holder_name"`
		} `json:"session"`
	}
	bob.Get("/api/v1/devices").Into(&fleet)
	var listed bool
	for _, d := range fleet {
		if d.ID == machine.DeviceID {
			listed = true
			require.NotNil(t, d.Session, "the fleet list shows the held host as held")
			assert.Equal(t, "Alice", d.Session.HolderName)
		}
	}
	assert.True(t, listed)
}

// directLinkOutcomes reads the four fleet-wide outcome counters from the product's metrics.
func (a *Technician) directLinkOutcomes() map[string]string {
	a.t.Helper()
	counts := map[string]string{}
	for _, line := range strings.Split(a.platformInstrumentation(), "\n") {
		rest, ok := strings.CutPrefix(line, "opengate_direct_link_outcomes_total")
		if !ok {
			continue
		}
		labels, value, _ := strings.Cut(rest, " ")
		counts[labels] = value
	}
	return counts
}

func TestDirectLinkOutcomesCountOncePerSessionAndOnlyFromTheHolder(t *testing.T) {
	t.Parallel()

	product := newProduct(t)
	alice, machine := contosoOnlineMachine(t, product, "contoso-desk-12")
	bob := product.Technician(alice.Customer)

	opened := alice.mustOpenSession(machine.DeviceID)
	report := func(who *Technician, outcome string) int {
		return who.Post("/api/v1/sessions/"+opened.Token+"/outcome",
			map[string]string{"outcome": outcome}).Status
	}

	assert.Equal(t, http.StatusNoContent, report(alice, "direct"))
	assert.Equal(t, http.StatusNoContent, report(alice, "direct"), "a repeated report is accepted")
	assert.Equal(t, http.StatusNoContent, report(alice, "dropped"))
	assert.Equal(t, http.StatusForbidden, report(bob, "no_route"), "only the holder reports")
	assert.Equal(t, http.StatusBadRequest, report(alice, "teleported"))

	assert.Equal(t, map[string]string{
		`{outcome="direct"}`:      "1",
		`{outcome="dropped"}`:     "1",
		`{outcome="no_route"}`:    "0",
		`{outcome="unsupported"}`: "0",
	}, alice.directLinkOutcomes(),
		"each outcome counts once per session, under no label but the outcome")
}

func TestACustomerFilterNarrowsAndDoesNotPermit(t *testing.T) {
	t.Parallel()

	product := newProduct(t)
	_, machine := contosoOnlineMachine(t, product, "contoso-desk-04")
	fabrikam := product.arrangeCustomer("Fabrikam")

	looking := product.Administrator(fabrikam)
	assert.Empty(t, looking.devices(), "Contoso's machine is not on Fabrikam's page")

	reply := looking.openSession(machine.DeviceID)
	assert.Equal(t, http.StatusCreated, reply.Status,
		"the customer filter narrows what is shown; the tenant is what permits")
}

func TestTheHelperOnTheMachinesPortTellsAnAskerTheAddressItAskedFrom(t *testing.T) {
	t.Parallel()
	product := newProduct(t)

	conn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	require.NoError(t, err)
	defer conn.Close()
	helper, err := net.ResolveUDPAddr("udp4", product.QUICAddr)
	require.NoError(t, err)

	request, err := stun.Build(stun.TransactionID, stun.BindingRequest)
	require.NoError(t, err)
	var reply stun.Message
	require.Eventually(t, func() bool {
		if _, err := conn.WriteTo(request.Raw, helper); err != nil {
			return false
		}
		buf := make([]byte, 1500)
		_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			return false
		}
		reply = stun.Message{Raw: buf[:n]}
		return reply.Decode() == nil
	}, eventually, poll, "the product answers an address request on the port machines connect to")

	assert.Equal(t, request.TransactionID, reply.TransactionID)
	var mapped stun.XORMappedAddress
	require.NoError(t, mapped.GetFrom(&reply))
	local := conn.LocalAddr().(*net.UDPAddr)
	assert.True(t, mapped.IP.Equal(local.IP))
	assert.Equal(t, local.Port, mapped.Port)
}
