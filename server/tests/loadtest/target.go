package main

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// Production is never a target of a load run, and that has to be true of the
// code rather than of somebody's attention: the way a generator ends up pointed
// at production is a URL in an environment variable, set at two in the morning
// while something is on fire.
//
// So this is an allowlist. A denylist would admit every hostname nobody has
// thought about yet, which is exactly the set a mistake comes from.

// allowedHosts are the names a load run may address. Each is a staging service
// or a local stack; none of them resolves to production.
var allowedHosts = []string{
	// The staging release's services, by every form of their in-cluster name.
	"opengate-staging-server",
	"opengate-staging-postgres",
	// A disposable stack a runner brought up, where compose names the service.
	"server",
	"postgres",
	// A local stack.
	"localhost",
}

// deniedNameFragments are namespaces and names that mean production wherever
// they appear. They are checked as well as the allowlist because a service is
// reachable by several forms of its own address, and a new form of a production
// address must not become allowable by being unfamiliar.
var deniedNameFragments = []string{
	".opengate.",
	".opengate:",
}

// CheckTarget reports whether a base URL is one a load run may address.
func CheckTarget(raw string) error {
	if raw == "" {
		return fmt.Errorf("no load-test target given")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("load-test target %q is not a URL: %w", raw, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("load-test target %q must be http or https", raw)
	}
	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("load-test target %q names no host", raw)
	}
	return checkHost(host, raw)
}

// CheckQUICAddress reports whether a host:port a load run would dial over QUIC
// is allowed. It reaches the same systems as an HTTP target, so it goes through
// the same list.
func CheckQUICAddress(raw string) error {
	if raw == "" {
		return fmt.Errorf("no load-test target given")
	}
	host, port, err := net.SplitHostPort(raw)
	if err != nil {
		return fmt.Errorf("load-test target %q is not a host:port: %w", raw, err)
	}
	if host == "" || port == "" {
		return fmt.Errorf("load-test target %q names no host and port", raw)
	}
	return checkHost(host, raw)
}

// checkHost is the single decision both entry points make, so the two cannot
// drift into allowing different things.
func checkHost(host, raw string) error {
	lowered := strings.ToLower(host)

	// A production name is refused first, so no allowlist entry can be a prefix
	// of one and admit it by accident.
	for _, fragment := range deniedNameFragments {
		if strings.Contains(lowered+":", fragment) {
			return refuse(raw)
		}
	}

	// A cluster addresses the staging server by pod IP, so private addresses
	// are allowed. A public address that happens to be numeric is somebody
	// else's machine and is not.
	if ip := net.ParseIP(lowered); ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() {
			return nil
		}
		return refuse(raw)
	}

	// A name matches on its first label, so the service's short name and every
	// qualified form of it are one entry rather than four.
	label, _, _ := strings.Cut(lowered, ".")
	for _, allowed := range allowedHosts {
		if label == allowed {
			return nil
		}
	}
	return refuse(raw)
}

func refuse(raw string) error {
	return fmt.Errorf("%q is not an allowed load-test target; allowed hosts are %v plus loopback and private addresses",
		raw, allowedHosts)
}
