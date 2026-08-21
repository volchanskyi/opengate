package acceptance

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// enrolmentToken is the credential an administrator hands somebody installing
// the agent. It is the only thing a machine being installed holds.
type enrolmentToken struct {
	ID    uuid.UUID `json:"id"`
	Token string    `json:"token"`
	Label string    `json:"label"`
}

// mintEnrolmentToken is what an administrator does before an installation:
// they create a token, and the install command carries it.
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

// devices is the list a technician's fleet page shows for their customer.
func (a *Technician) devices() []deviceSummary {
	a.t.Helper()
	var listed []deviceSummary
	reply := a.Get(a.InCustomer("/api/v1/devices"))
	require.Equalf(a.t, http.StatusOK, reply.Status, "reading the device list failed: %s", reply.Text())
	reply.Into(&listed)
	return listed
}

// TestAMachineEnrolsWithATokenAndAppearsOnline is the sentence Agent
// Deployment promises: an administrator mints an enrolment token, somebody
// installs the agent on a machine with it, and that machine shows up in the
// device list, online, under the right customer.
//
// Every leg of this had coverage before and the chain had none: no test in the
// tree referenced the enrolment endpoints at all, because the machine-side
// harness minted certificates straight from the certificate manager and
// pre-seeded the device row. What that skipped is exactly what an installation
// is.
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

// TestAnEnrolmentTokenUsedTwiceGivesTwoDistinctMachines is the case an
// administrator creates by pasting one install command into two machines. An
// unlimited-use token is allowed to serve both, but the second machine must
// never take over the first machine's identity — that would make one of the
// two invisible for ever.
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

// TestAnExhaustedEnrolmentTokenIsRefused pins the bound an administrator sets
// when they mean the command for one machine only. A refusal here is what
// stops a leaked install command becoming an open door.
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

// TestAnUnknownEnrolmentTokenIsRefused is the mistyped install command: the
// answer must be a refusal, not a certificate.
func TestAnUnknownEnrolmentTokenIsRefused(t *testing.T) {
	t.Parallel()

	product := newProduct(t)
	product.arrangeCustomer("Contoso")

	assert.Equal(t, http.StatusNotFound, product.enrolAttempt("not-a-real-token").Status)
}

// TestAMachineRebuiltWithANewCertificateIsTheSameMachine covers a rebuild: the
// endpoint is reimaged, keeps its identity, and asks to be signed again. It
// must come back as the machine it was, not as a second row beside itself.
func TestAMachineRebuiltWithANewCertificateIsTheSameMachine(t *testing.T) {
	t.Parallel()

	product := newProduct(t)
	contoso := product.arrangeCustomer("Contoso")
	admin := product.Administrator(contoso)
	token := admin.mintEnrolmentToken("rebuild")

	original := product.Machine(token.Token, "contoso-workstation")
	original.AwaitOnline()
	original.Disconnect()

	rebuilt := product.MachineWithIdentity(token.Token, original.DeviceID, "contoso-workstation")
	rebuilt.AwaitOnline()

	assert.Len(t, admin.devices(), 1, "a rebuilt machine is the same machine, not a duplicate")
}
