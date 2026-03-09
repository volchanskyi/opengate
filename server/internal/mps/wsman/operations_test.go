package wsman

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPowerStateConstants(t *testing.T) {
	assert.Equal(t, PowerState(2), PowerOn)
	assert.Equal(t, PowerState(5), PowerCycle)
	assert.Equal(t, PowerState(8), SoftOff)
	assert.Equal(t, PowerState(10), HardReset)
}

func TestPowerStateString(t *testing.T) {
	tests := []struct {
		state PowerState
		want  string
	}{
		{PowerOn, "PowerOn"},
		{PowerCycle, "PowerCycle"},
		{SoftOff, "SoftOff"},
		{HardReset, "HardReset"},
		{PowerState(99), "PowerState(99)"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.state.String())
		})
	}
}

func TestPowerActionEnvelope(t *testing.T) {
	body := PowerStateChangeBody(int(HardReset))
	env := Envelope(PowerMgmtResourceURI, PowerMgmtAction, "", body)
	s := string(env)
	assert.Contains(t, s, "<p:PowerState>10</p:PowerState>")
	assert.Contains(t, s, "CIM_PowerManagementService")
}

func TestDeviceInfoEnvelope(t *testing.T) {
	env := Envelope(ComputerSystemResourceURI,
		"http://schemas.xmlsoap.org/ws/2004/09/transfer/Get",
		`<w:Selector Name="Name">ManagedSystem</w:Selector>`,
		"")
	s := string(env)
	assert.Contains(t, s, "CIM_ComputerSystem")
	assert.Contains(t, s, "ManagedSystem")
}
