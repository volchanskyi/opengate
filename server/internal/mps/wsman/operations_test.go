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

func TestExtractXMLFieldWithNamespacePrefix(t *testing.T) {
	tests := []struct {
		name string
		xml  string
		tag  string
		want string
	}{
		{"plain tag", "<Name>TestHost</Name>", "Name", "TestHost"},
		{"p: prefix", "<p:Name>TestHost</p:Name>", "Name", "TestHost"},
		{"g: prefix", "<g:Model>Desktop</g:Model>", "Model", "Desktop"},
		{"h: prefix", "<h:Version>1.0</h:Version>", "Version", "1.0"},
		{"unknown prefix", "<x:Name>TestHost</x:Name>", "Name", ""},
		{"missing tag", "<Other>value</Other>", "Name", ""},
		{"missing close tag", "<Name>value", "Name", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractXMLField([]byte(tt.xml), tt.tag)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestExtractXMLFieldParsesEnabledState(t *testing.T) {
	// Valid EnabledState parses to int
	xml := []byte("<EnabledState>2</EnabledState>")
	stateStr := extractXMLField(xml, "EnabledState")
	assert.Equal(t, "2", stateStr)
}

func TestParseEnabledStateInvalid(t *testing.T) {
	// strconv.Atoi fails on non-numeric EnabledState — verify error path
	tests := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"non-numeric", "abc"},
		{"float", "2.5"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseEnabledState(tt.input)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "parse EnabledState")
		})
	}
}

func TestParseEnabledStateValid(t *testing.T) {
	state, err := parseEnabledState("2")
	assert.NoError(t, err)
	assert.Equal(t, PowerOn, state)
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
