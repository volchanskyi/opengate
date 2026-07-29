package agentapi

// Hardware-inventory ingest. The agent reports host facts plus the Intel AMT
// presence it reads off the Management Engine interface; the AMT model and
// firmware are written separately by the server's WSMAN query over a CIRA
// connection, so this path deliberately touches only the agent's own columns.

import (
	"context"
	"fmt"
	"math"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

func (a *AgentConn) handleHardwareReport(ctx context.Context, msg *protocol.ControlMessage) error {
	nis := make([]device.NetworkInterfaceInfo, len(msg.NetworkInterfaces))
	for i, ni := range msg.NetworkInterfaces {
		nis[i] = device.NetworkInterfaceInfo{
			Name: ni.Name,
			MAC:  ni.MAC,
			IPv4: ni.IPv4,
			IPv6: ni.IPv6,
		}
	}

	hw := &device.Hardware{
		DeviceID:          a.DeviceID,
		CPUModel:          msg.CPUModel,
		CPUCores:          int(msg.CPUCores), // uint32 -> int: always fits on supported (64-bit) platforms.
		RAMTotalMB:        clampInt64(msg.RAMTotalMB),
		DiskTotalMB:       clampInt64(msg.DiskTotalMB),
		DiskFreeMB:        clampInt64(msg.DiskFreeMB),
		NetworkInterfaces: nis,
		SystemUUID:        parseSystemUUID(msg.SystemUUID),
		AMTAvailable:      msg.AMTAvailable,
		AMTVersion:        msg.AMTVersion,
	}
	if err := a.hardware.Upsert(ctx, hw); err != nil {
		return fmt.Errorf("upsert hardware: %w", err)
	}

	a.logger.Debug("hardware report stored", "device_id", a.DeviceID)
	return nil
}

// parseSystemUUID validates the SMBIOS system UUID an agent reports. Anything
// unparseable becomes nil, which the hardware upsert reads as "the agent said
// nothing" and preserves the stored key rather than orphaning an AMT link. The
// agent already rejects the all-zero and all-ones firmware placeholders.
func parseSystemUUID(raw string) *uuid.UUID {
	if raw == "" {
		return nil
	}
	parsed, err := uuid.Parse(raw)
	if err != nil {
		return nil
	}
	return &parsed
}

// clampInt64 narrows uint64 to int64, capping at math.MaxInt64 to avoid sign flip.
func clampInt64(v uint64) int64 {
	if v > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(v)
}
