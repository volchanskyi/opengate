package acceptance

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/db"
)

// enrolmentToken is the credential a machine being installed presents to enrol.
type enrolmentToken struct {
	ID    uuid.UUID `json:"id"`
	Token string    `json:"token"`
	Label string    `json:"label"`
}

// mintEnrolmentToken creates the token an install command carries.
func (a *Technician) mintEnrolmentToken(label string) enrolmentToken {
	a.t.Helper()
	var token enrolmentToken
	reply := a.Post("/api/v1/enrollment-tokens", map[string]any{
		"label": label, "max_uses": 0, "expires_in_hours": 24,
	})
	require.Equalf(a.t, http.StatusCreated, reply.Status, "minting a token failed: %s", reply.Text())
	reply.Into(&token)
	require.NotEmpty(a.t, token.Token)
	return token
}

// deviceSummary is a machine as the device list shows it.
type deviceSummary struct {
	ID       uuid.UUID `json:"id"`
	Hostname string    `json:"hostname"`
	Status   string    `json:"status"`
}

func (a *Technician) devices() []deviceSummary {
	a.t.Helper()
	var listed []deviceSummary
	reply := a.Get(a.InCustomer("/api/v1/devices"))
	require.Equalf(a.t, http.StatusOK, reply.Status, "reading the device list failed: %s", reply.Text())
	reply.Into(&listed)
	return listed
}

func TestAMachineEnrolsWithATokenAndAppearsOnline(t *testing.T) {
	t.Parallel()

	product := newProduct(t)
	contoso := product.arrangeCustomer("Contoso")
	admin := product.Administrator(contoso)

	token := admin.mintEnrolmentToken("Head Office rollout")
	machine := product.Machine(token.Token, "contoso-reception-pc")

	machine.AwaitOnline()

	listed := admin.devices()
	require.Len(t, listed, 1, "the machine that enrolled is the machine in the list")
	assert.Equal(t, machine.DeviceID, listed[0].ID)
	assert.Equal(t, "contoso-reception-pc", listed[0].Hostname)
	assert.Equal(t, "online", listed[0].Status)
}

func TestAnEnrolmentTokenUsedTwiceGivesTwoDistinctMachines(t *testing.T) {
	t.Parallel()

	product := newProduct(t)
	contoso := product.arrangeCustomer("Contoso")
	admin := product.Administrator(contoso)

	token := admin.mintEnrolmentToken("shared install command")
	first := product.Machine(token.Token, "contoso-till-1")
	second := product.Machine(token.Token, "contoso-till-2")

	first.AwaitOnline()
	second.AwaitOnline()

	assert.NotEqual(t, first.DeviceID, second.DeviceID,
		"two installations are two machines, never one overwriting the other")
	assert.Len(t, admin.devices(), 2)
}

func TestAnExhaustedEnrolmentTokenIsRefused(t *testing.T) {
	t.Parallel()

	product := newProduct(t)
	contoso := product.arrangeCustomer("Contoso")
	admin := product.Administrator(contoso)

	var token enrolmentToken
	reply := admin.Post("/api/v1/enrollment-tokens", map[string]any{
		"label": "one machine only", "max_uses": 1, "expires_in_hours": 24,
	})
	require.Equal(t, http.StatusCreated, reply.Status)
	reply.Into(&token)

	product.Machine(token.Token, "contoso-laptop").AwaitOnline()

	assert.Equal(t, http.StatusGone, product.enrolAttempt(token.Token).Status,
		"a token that has been spent must not enrol a second machine")
}

func TestAnUnknownEnrolmentTokenIsRefused(t *testing.T) {
	t.Parallel()

	product := newProduct(t)
	product.arrangeCustomer("Contoso")

	assert.Equal(t, http.StatusNotFound, product.enrolAttempt("not-a-real-token").Status)
}

func TestAMachineRebuiltWithANewCertificateIsTheSameMachine(t *testing.T) {
	t.Parallel()

	product := newProduct(t)
	contoso := product.arrangeCustomer("Contoso")
	admin := product.Administrator(contoso)
	token := admin.mintEnrolmentToken("rebuild")

	original := product.Machine(token.Token, "contoso-workstation")
	original.AwaitOnline()
	original.Disconnect()

	// Only the rebuild sends this hostname, so it marks the rebuild's registration reaching the row.
	const reimaged = "contoso-workstation-reimaged"
	rebuilt := product.MachineWithIdentity(token.Token, original.DeviceID, reimaged)
	require.Eventually(t, func() bool {
		d, err := product.deviceRow(rebuilt.DeviceID)
		return err == nil && d.Hostname == reimaged && d.Status == db.StatusOnline
	}, eventually, poll, "the rebuilt machine must come back online under its own registration")

	assert.Len(t, admin.devices(), 1, "a rebuilt machine is the same machine, not a duplicate")

	// The replaced connection's teardown must not write the device offline.
	assert.Never(t, func() bool {
		d, err := product.deviceRow(rebuilt.DeviceID)
		return err == nil && d.Status != db.StatusOnline
	}, eventually, poll, "a machine that is connected stays online while the connection it replaced departs")
}
