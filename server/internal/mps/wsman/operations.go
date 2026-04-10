package wsman

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// WSMAN resource URIs for AMT operations.
const (
	PowerMgmtResourceURI     = "http://schemas.dmtf.org/wbem/wscim/1/cim-schema/2/CIM_PowerManagementService"
	PowerMgmtAction          = "http://schemas.dmtf.org/wbem/wscim/1/cim-schema/2/CIM_PowerManagementService/RequestPowerStateChange"
	ComputerSystemResourceURI = "http://schemas.dmtf.org/wbem/wscim/1/cim-schema/2/CIM_ComputerSystem"
	AMTSetupResourceURI      = "http://intel.com/wbem/wscim/1/amt-schema/1/AMT_SetupAndConfigurationService"
	TransferGetAction        = "http://schemas.xmlsoap.org/ws/2004/09/transfer/Get"
)

// PowerState represents an AMT power state for CIM_PowerManagementService.
type PowerState int

// Standard AMT power states.
const (
	PowerOn    PowerState = 2
	PowerCycle PowerState = 5
	SoftOff    PowerState = 8
	HardReset  PowerState = 10
)

// String returns the name of the power state.
func (ps PowerState) String() string {
	switch ps {
	case PowerOn:
		return "PowerOn"
	case PowerCycle:
		return "PowerCycle"
	case SoftOff:
		return "SoftOff"
	case HardReset:
		return "HardReset"
	default:
		return fmt.Sprintf("PowerState(%d)", int(ps))
	}
}

// DeviceInfo holds information queried from an AMT device via WSMAN.
type DeviceInfo struct {
	Hostname string
	Model    string
	Firmware string
}

// RequestPowerStateChange sends a power command to the AMT device.
func (c *Client) RequestPowerStateChange(ctx context.Context, state PowerState) error {
	body := PowerStateChangeBody(int(state))
	env := Envelope(PowerMgmtResourceURI, PowerMgmtAction, "", body)

	resp, err := c.Do(ctx, PowerMgmtAction, env)
	if err != nil {
		return fmt.Errorf("power state change: %w", err)
	}

	c.logger.Info("power state change response", "state", state, "resp_len", len(resp))
	return nil
}

// GetDeviceInfo queries CIM_ComputerSystem for device details.
func (c *Client) GetDeviceInfo(ctx context.Context) (*DeviceInfo, error) {
	selector := `<w:Selector Name="Name">ManagedSystem</w:Selector>`
	env := Envelope(ComputerSystemResourceURI, TransferGetAction, selector, "")

	resp, err := c.Do(ctx, TransferGetAction, env)
	if err != nil {
		return nil, fmt.Errorf("get device info: %w", err)
	}

	// Parse basic fields from the response XML.
	info := &DeviceInfo{}
	bodyXML, err := ParseEnvelopeBody(resp)
	if err != nil {
		c.logger.Warn("parse device info response", "error", err)
		return info, nil
	}

	info.Hostname = extractXMLField(bodyXML, "Name")
	info.Model = extractXMLField(bodyXML, "Model")
	return info, nil
}

// GetPowerState queries the current power state.
func (c *Client) GetPowerState(ctx context.Context) (PowerState, error) {
	selector := `<w:Selector Name="Name">ManagedSystem</w:Selector>`
	env := Envelope(ComputerSystemResourceURI, TransferGetAction, selector, "")

	resp, err := c.Do(ctx, TransferGetAction, env)
	if err != nil {
		return 0, fmt.Errorf("get power state: %w", err)
	}

	bodyXML, err := ParseEnvelopeBody(resp)
	if err != nil {
		return 0, err
	}

	stateStr := extractXMLField(bodyXML, "EnabledState")
	return parseEnabledState(stateStr)
}

// parseEnabledState converts a string EnabledState value to a PowerState.
func parseEnabledState(s string) (PowerState, error) {
	state, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("parse EnabledState %q: %w", s, err)
	}
	return PowerState(state), nil
}

// extractXMLField does a simple substring search for <tag>value</tag>.
// This avoids a full XML parse for simple flat responses.
func extractXMLField(data []byte, tag string) string {
	s := string(data)
	open := "<" + tag + ">"
	close := "</" + tag + ">"
	start := strings.Index(s, open)
	if start < 0 {
		// Try with namespace prefix (e.g., <p:Name>).
		for _, prefix := range []string{"p:", "g:", "h:"} {
			open = "<" + prefix + tag + ">"
			close = "</" + prefix + tag + ">"
			start = strings.Index(s, open)
			if start >= 0 {
				break
			}
		}
	}
	if start < 0 {
		return ""
	}
	start += len(open)
	end := strings.Index(s[start:], close)
	if end < 0 {
		return ""
	}
	return s[start : start+end]
}
