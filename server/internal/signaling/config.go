// Package signaling holds the ICE servers a browser tries to upgrade a relayed session to direct.
package signaling

// ICEServer is one STUN or TURN server with optional credentials.
type ICEServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

// Config is the ICE configuration the server hands a browser.
type Config struct {
	ICEServers []ICEServer
}

// DefaultConfig returns a Config naming Google's public STUN server.
func DefaultConfig() Config {
	return Config{
		ICEServers: []ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
		},
	}
}
