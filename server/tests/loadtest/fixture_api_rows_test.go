package main

import (
	"net/http"
	"strings"
)

// registerOnly makes filing answer as the server does for a machine that never registered.
func (f *fakeAPI) registerOnly() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows = map[string]bool{}
}

// land writes one machine's row, as its first registration does.
func (f *fakeAPI) land(deviceID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.rows != nil {
		f.rows[deviceID] = true
	}
}

// neverRegistered answers 404 for a machine with no row, in the server's own words.
func (f *fakeAPI) neverRegistered(w http.ResponseWriter, path string) bool {
	deviceID := strings.TrimSuffix(strings.TrimPrefix(path, "/api/v1/devices/"), "/organization")
	f.mu.Lock()
	missing := f.rows != nil && !f.rows[deviceID]
	f.mu.Unlock()
	if missing {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "device not found"})
	}
	return missing
}
