package main

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// allowedHosts are the names a load run may address: staging services and local stacks only,
// so a name outside the list is refused.
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

// deniedNameFragments are namespaces and names that mean production wherever they appear.
// They are checked beside the allowlist because a service has several forms of its address.
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

// CheckQUICAddress reports whether a host:port a load run would dial over QUIC is allowed,
// by the same list as an HTTP target.
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

	// A cluster addresses the staging server by pod IP, so only loopback and private
	// addresses are allowed.
	if ip := net.ParseIP(lowered); ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() {
			return nil
		}
		return refuse(raw)
	}

	// A name matches on its first label, so a service's short and qualified forms share one entry.
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
