package api

import (
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/signaling"
)

func deviceToAPI(d *db.Device) Device {
	return Device{
		Id:        d.ID,
		GroupId:   d.GroupID,
		Hostname:  d.Hostname,
		Os:        d.OS,
		Status:    DeviceStatus(d.Status),
		LastSeen:  d.LastSeen,
		CreatedAt: d.CreatedAt,
		UpdatedAt: d.UpdatedAt,
	}
}

func devicesToAPI(ds []*db.Device) []Device {
	out := make([]Device, len(ds))
	for i, d := range ds {
		out[i] = deviceToAPI(d)
	}
	return out
}

func groupToAPI(g *db.Group) Group {
	return Group{
		Id:        g.ID,
		Name:      g.Name,
		OwnerId:   g.OwnerID,
		CreatedAt: g.CreatedAt,
		UpdatedAt: g.UpdatedAt,
	}
}

func groupsToAPI(gs []*db.Group) []Group {
	out := make([]Group, len(gs))
	for i, g := range gs {
		out[i] = groupToAPI(g)
	}
	return out
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
	out := make([]User, len(us))
	for i, u := range us {
		out[i] = userToAPI(u)
	}
	return out
}

func sessionToAPI(s *db.AgentSession) AgentSession {
	return AgentSession{
		Token:     s.Token,
		DeviceId:  s.DeviceID,
		UserId:    s.UserID,
		CreatedAt: s.CreatedAt,
	}
}

func sessionsToAPI(ss []*db.AgentSession) []AgentSession {
	out := make([]AgentSession, len(ss))
	for i, s := range ss {
		out[i] = sessionToAPI(s)
	}
	return out
}

func permissionsToProtocol(p *Permissions) protocol.Permissions {
	if p == nil {
		return protocol.Permissions{}
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
