// Package signaling handles WebRTC SDP/ICE exchange between browsers and agents.
package signaling

import "time"

// ICEServer holds STUN/TURN server configuration.
type ICEServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

// Config holds signaling-related configuration.
type Config struct {
	// ICEServers is the list of STUN/TURN servers for WebRTC.
	ICEServers []ICEServer
	// UpgradeTimeout is how long to wait for the WebRTC upgrade to complete.
	UpgradeTimeout time.Duration
	// ICEGatherTimeout is how long to wait for ICE candidate gathering.
	ICEGatherTimeout time.Duration
}

// DefaultConfig returns a Config with sensible defaults.
// Uses Google's public STUN server.
func DefaultConfig() Config {
	return Config{
		ICEServers: []ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
		},
		UpgradeTimeout:   30 * time.Second,
		ICEGatherTimeout: 10 * time.Second,
	}
}
