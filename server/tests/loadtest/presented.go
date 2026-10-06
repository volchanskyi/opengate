package main

import (
	"fmt"
	"net/http"
)

// presentedAddressRangeSize is how many addresses 198.18.0.0/15 holds, the range RFC 2544
// reserves for benchmark traffic.
const presentedAddressRangeSize = 1 << 17

// presentedAddress derives a machine's address from its fleet index; a larger fleet wraps.
func presentedAddress(index int) string {
	offset := index % presentedAddressRangeSize
	if offset < 0 {
		offset += presentedAddressRangeSize
	}
	return fmt.Sprintf("198.%d.%d.%d", 18+(offset>>16), (offset>>8)&255, offset&255)
}

// presentAddress sets the forwarded-for header, and sets none when there is no address.
func presentAddress(request *http.Request, address string) {
	if address == "" {
		return
	}
	request.Header.Set("X-Forwarded-For", address)
}
