package wsman

import (
	"bytes"
	"encoding/xml"
	"fmt"
)

const soapNS = "http://www.w3.org/2003/05/soap-envelope"

// Envelope builds a WSMAN SOAP envelope with WS-Addressing headers.
func Envelope(resourceURI, action, selectorSet, body string) []byte {
	var selectors string
	if selectorSet != "" {
		selectors = fmt.Sprintf(`<w:SelectorSet>%s</w:SelectorSet>`, selectorSet)
	}
	return []byte(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<s:Envelope xmlns:s="%s"
  xmlns:a="http://schemas.xmlsoap.org/ws/2004/08/addressing"
  xmlns:w="http://schemas.dmtf.org/wbem/wsman/1/wsman.xsd"
  xmlns:p="%s">
<s:Header>
  <a:Action>%s</a:Action>
  <a:To>http://localhost:16992/wsman</a:To>
  <w:ResourceURI>%s</w:ResourceURI>
  <a:MessageID>1</a:MessageID>
  <a:ReplyTo><a:Address>http://schemas.xmlsoap.org/ws/2004/08/addressing/role/anonymous</a:Address></a:ReplyTo>
  %s
</s:Header>
<s:Body>%s</s:Body>
</s:Envelope>`, soapNS, resourceURI, action, resourceURI, selectors, body))
}

// PowerStateChangeBody builds the SOAP body for CIM_PowerManagementService.RequestPowerStateChange.
func PowerStateChangeBody(state int) string {
	return fmt.Sprintf(`<p:RequestPowerStateChange_INPUT>
  <p:PowerState>%d</p:PowerState>
  <p:ManagedElement>
    <a:Address>http://localhost:16992/wsman</a:Address>
    <a:ReferenceParameters>
      <w:ResourceURI>http://schemas.dmtf.org/wbem/wscim/1/cim-schema/2/CIM_ComputerSystem</w:ResourceURI>
      <w:SelectorSet><w:Selector Name="Name">ManagedSystem</w:Selector></w:SelectorSet>
    </a:ReferenceParameters>
  </p:ManagedElement>
</p:RequestPowerStateChange_INPUT>`, state)
}

// EnumerateBody builds a WSMAN Enumerate request body.
func EnumerateBody() string {
	return `<w:Enumerate xmlns:w="http://schemas.xmlsoap.org/ws/2004/09/enumeration"/>`
}

// PullBody builds a WSMAN Pull request body with an enumeration context.
func PullBody(enumContext string) string {
	return fmt.Sprintf(`<w:Pull xmlns:w="http://schemas.xmlsoap.org/ws/2004/09/enumeration">
  <w:EnumerationContext>%s</w:EnumerationContext>
  <w:MaxElements>999</w:MaxElements>
</w:Pull>`, enumContext)
}

// soapEnvelope is the minimal structure for parsing SOAP responses.
type soapEnvelope struct {
	XMLName xml.Name  `xml:"Envelope"`
	Body    *soapBody `xml:"Body"`
}

type soapBody struct {
	Inner []byte `xml:",innerxml"`
}

// ParseEnvelopeBody extracts the raw XML content inside <s:Body> from a SOAP envelope.
func ParseEnvelopeBody(data []byte) ([]byte, error) {
	var env soapEnvelope
	if err := xml.NewDecoder(bytes.NewReader(data)).Decode(&env); err != nil {
		return nil, fmt.Errorf("wsman: parse envelope: %w", err)
	}
	if env.Body == nil || len(env.Body.Inner) == 0 {
		return nil, fmt.Errorf("wsman: empty SOAP body")
	}
	return env.Body.Inner, nil
}
