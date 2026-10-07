package main

import (
	"encoding/json"
	"net/http"
)

// NewControl returns the shaper's command surface. No Ingress or Service publishes it, and it
// carries no authentication.
func NewControl(shaper *Shaper) http.Handler {
	mux := http.NewServeMux()

	// The runner asks /healthz directly because a scenario against a silent shaper measures nothing.
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
		// A mistyped instruction is refused at the call, so no scenario measures an unnamed impairment.
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

// methodIs refuses the wrong verb so a runner never believes it commanded a scenario it did not.
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
