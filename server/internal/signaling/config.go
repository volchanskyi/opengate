// Package signaling holds the ICE configuration a browser is handed when it
// asks to upgrade a relayed session to a direct one.
//
// The negotiation itself — the SDP offer and answer, the ICE candidates, the
// acknowledgements — happens between the two peers, carried as opaque frames
// through the relay pipe the server copies without decoding. The server's whole
// part in it is naming the STUN servers the browser should try, which is what
// this package is.
package signaling

// ICEServer holds STUN/TURN server configuration.
type ICEServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

// Config is what the server tells a browser about reaching a machine directly.
type Config struct {
	// ICEServers is the list of STUN/TURN servers for WebRTC.
	ICEServers []ICEServer
}

// DefaultConfig returns a Config with sensible defaults.
// Uses Google's public STUN server.
func DefaultConfig() Config {
	return Config{
		ICEServers: []ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
		},
	}
}
