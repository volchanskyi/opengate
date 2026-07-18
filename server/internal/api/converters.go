package api

import (
	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/audit"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/inventory"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/session"
	"github.com/volchanskyi/opengate/server/internal/signaling"
	"github.com/volchanskyi/opengate/server/internal/updater"
)

// mapSlice converts a slice of one type to another using the given function.
func mapSlice[S, D any](items []S, fn func(S) D) []D {
	out := make([]D, len(items))
	for i, item := range items {
		out[i] = fn(item)
	}
	return out
}

func deviceToAPI(d *device.Device) Device {
	dev := Device{
		Id:           d.ID,
		GroupId:      d.GroupID,
		Hostname:     d.Hostname,
		Os:           d.OS,
		AgentVersion: d.AgentVersion,
		Capabilities: d.Capabilities,
		Status:       DeviceStatus(d.Status),
		LastSeen:     d.LastSeen,
		CreatedAt:    d.CreatedAt,
		UpdatedAt:    d.UpdatedAt,
	}
	if d.OsDisplay != "" {
		dev.OsDisplay = &d.OsDisplay
	}
	// The maintenance fields travel together: present only while a device is in
	// maintenance, omitted (falsy) for the common Active case, matching the
	// os_display / anomaly_rate omit-zero convention.
	if d.MaintenanceOn {
		on := true
		dev.MaintenanceOn = &on
		dev.MaintenanceSince = d.MaintenanceSince
		dev.MaintenanceBy = d.MaintenanceBy
		if d.MaintenanceReason != "" {
			dev.MaintenanceReason = &d.MaintenanceReason
		}
	}
	return dev
}

func devicesToAPI(ds []*device.Device) []Device {
	return mapSlice(ds, deviceToAPI)
}

func groupToAPI(g *device.Group) Group {
	return Group{
		Id:        g.ID,
		Name:      g.Name,
		OwnerId:   g.OwnerID,
		CreatedAt: g.CreatedAt,
		UpdatedAt: g.UpdatedAt,
	}
}

func groupsToAPI(gs []*device.Group) []Group {
	return mapSlice(gs, groupToAPI)
}

func userToAPI(u *db.User) User {
	return User{
		Id:          u.ID,
		Email:       u.Email,
		DisplayName: u.DisplayName,
		IsAdmin:     u.IsAdmin,
		CreatedAt:   u.CreatedAt,
		UpdatedAt:   u.UpdatedAt,
	}
}

func usersToAPI(us []*db.User) []User {
	return mapSlice(us, userToAPI)
}

func sessionToAPI(s *session.Session) AgentSession {
	return AgentSession{
		Token:     s.Token,
		DeviceId:  s.DeviceID,
		UserId:    s.UserID,
		CreatedAt: s.CreatedAt,
	}
}

func sessionsToAPI(ss []*session.Session) []AgentSession {
	return mapSlice(ss, sessionToAPI)
}

func permissionsToProtocol(p *Permissions) protocol.Permissions {
	if p == nil {
		// Default to all-true so sessions work when no permissions are specified.
		return protocol.Permissions{
			Desktop:   true,
			Terminal:  true,
			FileRead:  true,
			FileWrite: true,
			Input:     true,
		}
	}
	return protocol.Permissions{
		Desktop:   derefBool(p.Desktop),
		Terminal:  derefBool(p.Terminal),
		FileRead:  derefBool(p.FileRead),
		FileWrite: derefBool(p.FileWrite),
		Input:     derefBool(p.Input),
	}
}

func derefBool(b *bool) bool {
	if b == nil {
		return false
	}
	return *b
}

func derefStr[T ~string](s *T) string {
	if s == nil {
		return ""
	}
	return string(*s)
}

func derefInt(p *int, fallback int) int {
	if p == nil {
		return fallback
	}
	return *p
}

func auditEventToAPI(e *audit.Event) AuditEvent {
	return AuditEvent{
		Id:        e.ID,
		UserId:    e.UserID,
		Action:    e.Action,
		Target:    e.Target,
		Details:   e.Details,
		CreatedAt: e.CreatedAt,
	}
}

func auditEventsToAPI(es []*audit.Event) []AuditEvent {
	return mapSlice(es, auditEventToAPI)
}

func manifestToAPI(m *updater.Manifest) AgentManifest {
	return AgentManifest{
		Version:   m.Version,
		Os:        m.OS,
		Arch:      m.Arch,
		Url:       m.URL,
		Sha256:    m.SHA256,
		Signature: m.Signature,
		CreatedAt: m.CreatedAt,
	}
}

func manifestsToAPI(ms []*updater.Manifest) []AgentManifest {
	return mapSlice(ms, manifestToAPI)
}

// deviceInventoryToAPI maps the tenant-scoped discovered components of a device
// into the flat inventory item list the API exposes.
func deviceInventoryToAPI(deviceID uuid.UUID, components []inventory.Component) DeviceInventory {
	return DeviceInventory{
		DeviceId: deviceID,
		Items:    mapSlice(components, inventoryComponentToAPI),
	}
}

func inventoryComponentToAPI(c inventory.Component) InventoryItem {
	return InventoryItem{
		Kind:      InventoryItemKind(c.Kind),
		Name:      c.Name,
		Version:   c.Version,
		Port:      int(c.Port),
		Proto:     c.Proto,
		State:     c.State,
		Runtime:   c.Runtime,
		Image:     c.Image,
		FirstSeen: c.FirstSeen,
		LastSeen:  c.LastSeen,
	}
}

func deviceHardwareToAPI(hw *device.Hardware) DeviceHardware {
	return DeviceHardware{
		DeviceId:          hw.DeviceID,
		CpuModel:          hw.CPUModel,
		CpuCores:          hw.CPUCores,
		RamTotalMb:        hw.RAMTotalMB,
		DiskTotalMb:       hw.DiskTotalMB,
		DiskFreeMb:        hw.DiskFreeMB,
		NetworkInterfaces: mapSlice(hw.NetworkInterfaces, networkInterfaceToAPI),
		UpdatedAt:         hw.UpdatedAt,
	}
}

func networkInterfaceToAPI(ni device.NetworkInterfaceInfo) NetworkInterfaceInfo {
	return NetworkInterfaceInfo{
		Name: ni.Name,
		Mac:  ni.MAC,
		Ipv4: ni.IPv4,
		Ipv6: ni.IPv6,
	}
}

func deviceLogsToAPI(entries []device.LogEntry, total int, filter device.LogFilter) DeviceLogsResponse {
	apiEntries := make([]DeviceLogEntry, len(entries))
	for i, e := range entries {
		apiEntries[i] = DeviceLogEntry{
			Timestamp: e.Timestamp,
			Level:     e.Level,
			Target:    e.Target,
			Message:   e.Message,
		}
	}
	hasMore := filter.Offset+filter.Limit < total
	return DeviceLogsResponse{
		Entries: apiEntries,
		Total:   total,
		HasMore: hasMore,
	}
}

func iceServersToAPI(servers []signaling.ICEServer) []ICEServer {
	out := make([]ICEServer, len(servers))
	for i, srv := range servers {
		out[i] = ICEServer{Urls: srv.URLs}
		if srv.Username != "" {
			out[i].Username = &srv.Username
		}
		if srv.Credential != "" {
			out[i].Credential = &srv.Credential
		}
	}
	return out
}
