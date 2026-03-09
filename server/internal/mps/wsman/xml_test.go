package wsman

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnvelopeStructure(t *testing.T) {
	env := Envelope(
		"http://schemas.dmtf.org/wbem/wscim/1/cim-schema/2/CIM_ComputerSystem",
		"http://schemas.xmlsoap.org/ws/2004/09/transfer/Get",
		"",
		"",
	)
	s := string(env)
	assert.Contains(t, s, "<s:Envelope")
	assert.Contains(t, s, "CIM_ComputerSystem")
	assert.Contains(t, s, "transfer/Get")
	assert.Contains(t, s, "</s:Envelope>")
}

func TestPowerStateChangeBody(t *testing.T) {
	body := PowerStateChangeBody(8)
	assert.Contains(t, body, "<p:PowerState>8</p:PowerState>")
	assert.Contains(t, body, "RequestPowerStateChange")
}

func TestParseEnvelopeBody(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope">
<s:Body>
<p:Response><p:ReturnValue>0</p:ReturnValue></p:Response>
</s:Body>
</s:Envelope>`

	body, err := ParseEnvelopeBody([]byte(xml))
	require.NoError(t, err)
	assert.Contains(t, string(body), "ReturnValue")
}

func TestParseEnvelopeBodyMissing(t *testing.T) {
	xml := `<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"></s:Envelope>`
	_, err := ParseEnvelopeBody([]byte(xml))
	assert.Error(t, err)
}

func TestEnumerateBody(t *testing.T) {
	body := EnumerateBody()
	assert.True(t, strings.Contains(body, "Enumerate"))
}

func TestPullBody(t *testing.T) {
	body := PullBody("ctx-123")
	assert.Contains(t, body, "ctx-123")
	assert.Contains(t, body, "Pull")
}
