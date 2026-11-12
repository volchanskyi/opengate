package acceptance

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// awaitAnswer waits for the product's answer to the alert the machine offered as id.
func (m *Machine) awaitAnswer(id string) *protocol.ControlMessage {
	m.t.Helper()
	var found *protocol.ControlMessage
	require.Eventuallyf(m.t, func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		for _, msg := range m.inbox {
			if msg.Type == protocol.MsgAlertAnswer && msg.AlertID == id {
				found = msg
				return true
			}
		}
		return false
	}, eventually, poll, "the product never answered the alert offered as %s", id)
	return found
}

func TestAMachineIsToldItsAlertWasStoredAndAnOfferAgainLandsOnce(t *testing.T) {
	t.Parallel()

	product := newProduct(t)
	contoso := product.arrangeCustomer("Contoso")
	admin := product.Administrator(contoso)
	machine := product.Machine(admin.mintEnrolmentToken("Head Office").Token, "contoso-build-agent",
		protocol.CapTerminal, protocol.CapThresholdAlerts, protocol.CapAlerts, protocol.CapAlertAnswers)
	machine.AwaitOnline()

	alert := machine.cpuAlert("indexer")
	machine.Send(alert)
	assert.Equal(t, string(protocol.AlertAnswerStored), machine.awaitAnswer(alert.AlertID).Reason,
		"the machine is told its alert is stored, so it can stop holding it")

	// A machine that never heard the answer offers the same alert again under a new id.
	again := *alert
	again.AlertID = uuid.NewString()
	machine.Send(&again)
	assert.Equal(t, string(protocol.AlertAnswerStored), machine.awaitAnswer(again.AlertID).Reason,
		"an alert already stored is answered stored, so the machine stops offering it")

	room := admin.awaitIncident()
	assert.Equal(t, triageRule, room.RuleID)
	assert.Equal(t, 1, room.Occurrences, "the alert offered twice lands once")
}

func TestAnAlertForARuleTheServerDoesNotShipIsRefusedForGood(t *testing.T) {
	t.Parallel()

	product := newProduct(t)
	contoso := product.arrangeCustomer("Contoso")
	admin := product.Administrator(contoso)
	machine := product.Machine(admin.mintEnrolmentToken("Head Office").Token, "contoso-sql02",
		protocol.CapTerminal, protocol.CapThresholdAlerts, protocol.CapAlerts, protocol.CapAlertAnswers)
	machine.AwaitOnline()

	alert := machine.cpuAlert("indexer")
	alert.RuleID = "contoso-homegrown-rule"
	machine.Send(alert)

	assert.Equal(t, string(protocol.AlertAnswerRefusedForGood), machine.awaitAnswer(alert.AlertID).Reason,
		"an alert no offer again could store is refused for good, so the machine lets it go")
	// The answer follows the refusal, so the queue already reflects it.
	assert.Empty(t, admin.triageQueue(), "a refused alert opens no room")
}
