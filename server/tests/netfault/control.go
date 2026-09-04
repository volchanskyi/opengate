package main

import (
	"encoding/json"
	"net/http"
)

// NewControl is the shaper's command surface: how the runner names the
// impairment a scenario is in, re-addresses mid-connection, and reads back what
// the shaper did with the datagrams it handled.
//
// It is cluster-internal — no Ingress route and no Service publishes it — so it
// carries no authentication of its own. What guards it is that nothing outside
// the namespace can reach it, which is the same guarantee the load harness's
// own in-cluster surfaces rest on.
func NewControl(shaper *Shaper) http.Handler {
	mux := http.NewServeMux()

	// Whether the shaper is answering at all is the difference between a
	// scenario that measured the product and one that measured nothing, so the
	// runner asks directly rather than inferring it from a counter that happens
	// to come back.
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if !methodIs(w, r, http.MethodGet) {
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("/counters", func(w http.ResponseWriter, r *http.Request) {
		if !methodIs(w, r, http.MethodGet) {
			return
		}
		writeJSON(w, shaper.Counters())
	})

	mux.HandleFunc("/impair", func(w http.ResponseWriter, r *http.Request) {
		if !methodIs(w, r, http.MethodPost) {
			return
		}
		var profile Profile
		if err := json.NewDecoder(r.Body).Decode(&profile); err != nil {
			http.Error(w, "that is not an impairment: "+err.Error(), http.StatusBadRequest)
			return
		}
		// The refusal is the point. A scenario that mistyped its instruction
		// must fail where it was typed, rather than running as whatever the
		// shaper made of the number and producing a measurement of some
		// impairment nobody named.
		if err := shaper.SetProfile(profile); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, shaper.Counters())
	})

	mux.HandleFunc("/rebind", func(w http.ResponseWriter, r *http.Request) {
		if !methodIs(w, r, http.MethodPost) {
			return
		}
		if err := shaper.Rebind(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, shaper.Counters())
	})

	return mux
}

// methodIs refuses the wrong verb, because a runner that reaches one is a
// runner that thinks it commanded a scenario it did not command, and the phase
// it goes on to measure is the phase before.
func methodIs(w http.ResponseWriter, r *http.Request, want string) bool {
	if r.Method != want {
		http.Error(w, "this endpoint answers "+want, http.StatusMethodNotAllowed)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}
