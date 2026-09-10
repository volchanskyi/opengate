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

// fakeAPI is a stand-in for the server, recording what the builder asked it for.
// It answers the handful of calls a fixture needs and nothing else, so a builder
// that reaches for a surface nobody agreed to gets a 404 and the test says so.
type fakeAPI struct {
	mu sync.Mutex

	loggedIn      bool
	organizations []string
	sites         []string
	registered    []string
	tokenLabels   []string
	tokenHours    []int
	// filedToCustomer and filedToSite are the two halves of filing a machine,
	// recorded apart because a machine under the right customer with no site is
	// exactly the state the browser-side scenarios cannot read.
	filedToCustomer []filedCall
	filedToSite     []filedCall

	// failAt makes one path answer 500, so the builder's error handling is
	// exercised rather than assumed.
	failAt string
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

// fleetUnderTest is one built fleet and everything a case needs to assert about
// it: the requests the server saw, the plan it came from, the fixture it became,
// and the client that built it. Producing these took six lines in every case,
// which buried what each case was actually about.
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
