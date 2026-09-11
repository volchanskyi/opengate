package main

import (
	"fmt"
	"net/http"
)

// A run presents one address per machine, so the request allowance it spends is
// the one a real estate would spend.
//
// The server counts requests per address, at about a hundred a second. A fleet
// arriving from one pod spends one allowance between all of it — and every
// arrival costs an enrolment plus the two requests that file the machine under
// its customer and into its building. Two profiles sit above that ceiling on
// arrivals alone: spike, whose whole subject is fifteen hundred machines coming
// back at once when a site regains its link, and the largest volume leg. What
// they would otherwise measure is the allowance rather than the server.
//
// The address is only believed from a peer the deployment has named as a proxy,
// so this claims nothing anywhere that has not said so. See
// server/internal/api/proxytrust.go.

// presentedAddressRangeSize is how many addresses 198.18.0.0/15 holds. The
// range exists for exactly this — RFC 2544 reserves it for benchmark traffic —
// so a synthetic address can never be somebody real.
const presentedAddressRangeSize = 1 << 17

// presentedAddress is the address one machine presents, derived from its place
// in the fleet so that it is the same address from the machine's enrolment to
// its filing. A fleet larger than the range wraps, which costs two machines a
// shared allowance rather than putting either outside the range.
func presentedAddress(index int) string {
	offset := index % presentedAddressRangeSize
	if offset < 0 {
		offset += presentedAddressRangeSize
	}
	return fmt.Sprintf("198.%d.%d.%d", 18+(offset>>16), (offset>>8)&255, offset&255)
}

// presentAddress writes the address onto a request, or writes nothing when
// there is none to present. An empty header is worse than an absent one: a
// server that believes this peer falls back to the peer either way, and the
// empty header is left for whoever reads the request afterwards to interpret.
func presentAddress(request *http.Request, address string) {
	if address == "" {
		return
	}
	request.Header.Set("X-Forwarded-For", address)
}
