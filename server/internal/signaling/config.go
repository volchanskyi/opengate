// Package signaling holds what the two sides of a session are told about where to meet: the
// address a host dials for the relay, and the helpers each side asks for its public address.
package signaling

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// ErrAgentRelayRequired is returned when no host relay address is configured.
var ErrAgentRelayRequired = errors.New("the address hosts dial for the relay is required")

// relayPath is where the relay listens; a host's relay URL is the base plus this and the token.
const relayPath = "/ws/relay/"

// Config is what the server hands both sides of a session about where to meet.
type Config struct {
	// AgentRelayBase is the scheme and authority a host dials for the relay. The browser's
	// address is built from its own request, so this one never reaches a browser.
	AgentRelayBase string
	// Helpers are the STUN URLs each side asks for its public address, in the order tried.
	Helpers []string
}

// New reads the host relay address, e.g. "wss://opengate.example.com", and a comma-separated
// helper list, e.g. "stun:quic.example.com:9090"; an empty helper list is valid.
func New(agentRelayBase, helpers string) (Config, error) {
	base, err := parseRelayBase(agentRelayBase)
	if err != nil {
		return Config{}, err
	}
	list := []string{}
	for entry := range strings.SplitSeq(helpers, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if err := checkHelper(entry); err != nil {
			return Config{}, err
		}
		list = append(list, entry)
	}
	return Config{AgentRelayBase: base, Helpers: list}, nil
}

// AgentRelayURL is the relay address a host dials for the session token.
func (c Config) AgentRelayURL(token string) string {
	return c.AgentRelayBase + relayPath + token
}

// parseRelayBase accepts a ws or wss address naming a host and nothing after it.
func parseRelayBase(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", ErrAgentRelayRequired
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("host relay address %q: %w", raw, err)
	}
	switch {
	case u.Scheme != "ws" && u.Scheme != "wss":
		return "", fmt.Errorf("host relay address %q: the scheme is ws or wss", raw)
	case u.Host == "":
		return "", fmt.Errorf("host relay address %q names no host", raw)
	case u.User != nil, u.RawQuery != "", u.Fragment != "", strings.Trim(u.Path, "/") != "":
		return "", fmt.Errorf("host relay address %q carries more than a scheme and a host", raw)
	}
	return u.Scheme + "://" + u.Host, nil
}

// checkHelper accepts "stun:host:port", the one kind of helper the server answers as.
func checkHelper(raw string) error {
	rest, ok := strings.CutPrefix(raw, "stun:")
	if !ok {
		return fmt.Errorf("helper %q: only stun: helpers are served", raw)
	}
	host, port, err := net.SplitHostPort(rest)
	if err != nil || host == "" {
		return fmt.Errorf("helper %q: the form is stun:host:port", raw)
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("helper %q: the port is a number from 1 to 65535", raw)
	}
	return nil
}
