package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// filedCall is one filing request: which machine, and what it was filed under.
type filedCall struct {
	deviceID string
	target   string
}

// fakeAPI stands in for the server, records each call and answers 404 to any other.
type fakeAPI struct {
	mu sync.Mutex

	loggedIn      bool
	organizations []string
	sites         []string
	registered    []string
	tokenLabels   []string
	tokenHours    []int
	// filedToCustomer and filedToSite record the two halves of filing a machine apart.
	filedToCustomer []filedCall
	filedToSite     []filedCall

	// failAt makes one path answer 500.
	failAt string

	// rowLandsAfter is how many filing attempts answer as for a machine whose row is not written.
	// The row lands after the register frame is read, which can follow the filing request.
	rowLandsAfter int
	// filingAttempts counts every attempt at the filing path, refused ones included.
	filingAttempts int
}

func (f *fakeAPI) handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		if f.fail(w, "/api/v1/auth/login") {
			return
		}
		f.mu.Lock()
		f.loggedIn = true
		f.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]string{"token": "operator-token"})
	})

	mux.HandleFunc("/api/v1/auth/register", func(w http.ResponseWriter, r *http.Request) {
		if f.fail(w, "/api/v1/auth/register") {
			return
		}
		var body struct {
			Email string `json:"email"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.registered = append(f.registered, body.Email)
		f.mu.Unlock()
		writeJSON(w, http.StatusCreated, map[string]string{"token": "member-token"})
	})

	mux.HandleFunc("/api/v1/organizations", func(w http.ResponseWriter, r *http.Request) {
		if f.fail(w, "/api/v1/organizations") {
			return
		}
		var body struct {
			Name string `json:"name"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.organizations = append(f.organizations, body.Name)
		id := fmt.Sprintf("org-%d", len(f.organizations))
		f.mu.Unlock()
		writeJSON(w, http.StatusCreated, map[string]string{"id": id, "name": body.Name})
	})

	mux.HandleFunc("/api/v1/sites", func(w http.ResponseWriter, r *http.Request) {
		if f.fail(w, "/api/v1/sites") {
			return
		}
		var body struct {
			Name           string `json:"name"`
			OrganizationID string `json:"organization_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.sites = append(f.sites, body.Name)
		id := fmt.Sprintf("site-%d", len(f.sites))
		f.mu.Unlock()
		writeJSON(w, http.StatusCreated, map[string]string{
			"id": id, "name": body.Name, "organization_id": body.OrganizationID,
		})
	})

	mux.HandleFunc("/api/v1/enrollment-tokens", func(w http.ResponseWriter, r *http.Request) {
		if f.fail(w, "/api/v1/enrollment-tokens") {
			return
		}
		var body struct {
			Label          string `json:"label"`
			ExpiresInHours int    `json:"expires_in_hours"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.tokenLabels = append(f.tokenLabels, body.Label)
		f.tokenHours = append(f.tokenHours, body.ExpiresInHours)
		f.mu.Unlock()
		writeJSON(w, http.StatusCreated, map[string]string{"id": "tok-1", "token": "enrol-secret"})
	})

	mux.HandleFunc("/api/v1/devices/", func(w http.ResponseWriter, r *http.Request) {
		if f.fail(w, "/api/v1/devices/") {
			return
		}
		if f.rowHasNotLanded(w) {
			return
		}
		var body struct {
			OrganizationID string  `json:"organization_id"`
			SiteID         *string `json:"site_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)

		f.mu.Lock()
		if strings.HasSuffix(r.URL.Path, "/organization") {
			f.filedToCustomer = append(f.filedToCustomer, filedCall{
				deviceID: strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1/devices/"), "/organization"),
				target:   body.OrganizationID,
			})
		} else {
			site := ""
			if body.SiteID != nil {
				site = *body.SiteID
			}
			f.filedToSite = append(f.filedToSite, filedCall{
				deviceID: strings.TrimPrefix(r.URL.Path, "/api/v1/devices/"),
				target:   site,
			})
		}
		f.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]string{"id": "device"})
	})

	return mux
}

// rowHasNotLanded answers as the server does for a machine it has not written yet, using
// the server's own words for the missed lookup.
func (f *fakeAPI) rowHasNotLanded(w http.ResponseWriter) bool {
	f.mu.Lock()
	f.filingAttempts++
	notYet := f.filingAttempts <= f.rowLandsAfter
	f.mu.Unlock()
	if notYet {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "device not found"})
		return true
	}
	return false
}

// attemptsAtFiling is how many times the filing path was asked, refusals
// included.
func (f *fakeAPI) attemptsAtFiling() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.filingAttempts
}

func (f *fakeAPI) fail(w http.ResponseWriter, path string) bool {
	f.mu.Lock()
	shouldFail := f.failAt == path
	f.mu.Unlock()
	if shouldFail {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "boom"})
		return true
	}
	return false
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// newFixtureClient points a client at a fake server that is torn down with the
// case that asked for it.
func newFixtureClient(t *testing.T, api *fakeAPI) *FixtureClient {
	t.Helper()
	server := httptest.NewServer(api.handler())
	t.Cleanup(server.Close)
	return NewFixtureClient(server.URL)
}

// newFixtureClientForRun is the same, for a run whose length the credential has
// to cover.
func newFixtureClientForRun(t *testing.T, api *fakeAPI, runFor time.Duration) *FixtureClient {
	t.Helper()
	server := httptest.NewServer(api.handler())
	t.Cleanup(server.Close)
	return NewFixtureClientForRun(server.URL, runFor)
}

// hoursAsked is the lifetime asked for each credential the builder minted.
func (f *fakeAPI) hoursAsked() []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]int(nil), f.tokenHours...)
}

// fleetUnderTest is one built fleet with the requests the server saw, its plan, its
// fixture and the client that built it.
type fleetUnderTest struct {
	api     *fakeAPI
	client  *FixtureClient
	plan    FixturePlan
	fixture BuiltFixture
}

// buildFleet plans a fleet of the given size and walks it through a fake server.
func buildFleet(t *testing.T, size FixtureSize, seed uint64) fleetUnderTest {
	t.Helper()

	plan, err := PlanFixture(size, seed)
	require.NoError(t, err)

	api := &fakeAPI{}
	client := newFixtureClient(t, api)
	require.NoError(t, client.SignIn("admin@service.invalid", "secret"))

	built, err := client.BuildFixture(plan)
	require.NoError(t, err)
	return fleetUnderTest{api: api, client: client, plan: plan, fixture: built}
}
