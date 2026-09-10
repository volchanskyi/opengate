package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// A plan is not a fleet. PlanFixture decides what a fleet should be — how many
// customers, how the machines are spread between them, how many people look
// after them — and deciding it changes nothing in any database. Something has to
// walk that decision through the server, and this is it.
//
// It goes through the same interface a technician uses. A loader that wrote rows
// straight into the database would be faster and would describe a fleet shaped
// by what the loader believes the schema means; the two drift, quietly, and the
// first thing to notice is a measurement nobody can explain. Everything here is
// an ordinary request.
//
// Machines are the exception, and only in where they come from: a machine exists
// because one connected and registered, so the fixture mints the credential an
// installer would spend and the harness enrols with it. Filing each machine
// under its customer afterwards is an ordinary request like the rest.

// fixtureRequestTimeout bounds one call. A fixture is thousands of them, and one
// that hangs would hold the whole build with nothing to say about why.
const fixtureRequestTimeout = 30 * time.Second

// fixturePassword is the password every account a run creates is created with.
// These accounts live for one run inside an environment a run empties, and the
// value is here rather than generated so a cleanup that has to sign in as one
// can.
const fixturePassword = "LoadTestPass123!"

// FixtureClient drives the public API as one signed-in administrator.
type FixtureClient struct {
	baseURL string
	http    *http.Client
	token   string
	// runFor is how long the run that spends this fixture's credential lasts,
	// which is what that credential has to outlive.
	runFor time.Duration
}

// NewFixtureClient builds a client against one server, for a run that has
// declared no length.
func NewFixtureClient(baseURL string) *FixtureClient {
	return NewFixtureClientForRun(baseURL, 0)
}

// NewFixtureClientForRun builds a client for a run of a known length, so the
// credential the fleet enrols with can be made to outlive it.
func NewFixtureClientForRun(baseURL string, runFor time.Duration) *FixtureClient {
	return &FixtureClient{
		baseURL: baseURL,
		http:    &http.Client{Timeout: fixtureRequestTimeout},
		runFor:  runFor,
	}
}

// Token is the session this client holds, empty before it has signed in.
func (c *FixtureClient) Token() string { return c.token }

// BuiltCustomer is one customer as it exists on the server, with the sites it
// was given and the share of the fleet it is to hold.
type BuiltCustomer struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	SiteIDs []string `json:"site_ids"`
	Devices int      `json:"devices"`
}

// BuiltFixture is a plan that now exists.
type BuiltFixture struct {
	Size      FixtureSize     `json:"size"`
	Seed      uint64          `json:"seed"`
	Customers []BuiltCustomer `json:"customers"`
	Users     []string        `json:"users"`
	Sites     int             `json:"sites"`
	// PlannedDevices is how many machines the fleet is to hold. The machines
	// themselves arrive by enrolling, which is the harness's job, so this is the
	// number it is given rather than a number of rows already written.
	PlannedDevices int `json:"planned_devices"`
	// EnrollmentToken is the credential those machines spend. It is short-lived
	// and belongs to this run alone.
	EnrollmentToken string `json:"-"`
}

// FixtureCleanupManifest is what a built fixture obliges the run to remove. It
// carries the customers as well as the accounts, because a customer created
// through the API is not selected by any address pattern.
type FixtureCleanupManifest struct {
	Marker        string   `json:"marker"`
	Tenant        string   `json:"tenant"`
	Users         []string `json:"users"`
	Organizations []string `json:"organizations"`
	Devices       int      `json:"devices"`
}

// CleanupManifest is what this fixture obliges the run to remove.
func (b BuiltFixture) CleanupManifest() FixtureCleanupManifest {
	organizations := make([]string, 0, len(b.Customers))
	for _, customer := range b.Customers {
		organizations = append(organizations, customer.ID)
	}
	return FixtureCleanupManifest{
		Marker:        loadTestMarker,
		Tenant:        fmt.Sprintf("%s-tenant", loadTestMarker),
		Users:         append([]string(nil), b.Users...),
		Organizations: organizations,
		Devices:       b.PlannedDevices,
	}
}

// Counts is this fixture in the shape a bundle records it.
//
// The machine count it can state is the planned one: the machines themselves
// arrive by enrolling, which happens after this and may fall short. Reporting
// the plan as the fleet is how a bundle came to say two thousand machines while
// the database, weighed in the same job, held five hundred — so the fleet that
// exists is filled in by whoever counted the arrivals.
func (b BuiltFixture) Counts() FixtureCounts {
	return FixtureCounts{
		Size: b.Size,
		// One. Load identities live in the default tenant because no interface
		// asks for a second one; the debt register carries that and its trigger.
		Tenants:        1,
		Customers:      len(b.Customers),
		Sites:          b.Sites,
		Users:          len(b.Users),
		PlannedDevices: b.PlannedDevices,
	}
}

// EnsureAdmin opens the administrator session the build needs, either by signing
// in or — in an environment that starts empty — by registering the first account,
// which the server promotes to administrator because it is the first.
//
// Which of the two applies is stated rather than discovered. A client that tried
// one and fell back to the other would create an account on an environment that
// already had one, and that account would be an ordinary member with no way to
// create a customer; the run would then fail several steps later with a refusal
// that names the wrong cause.
func (c *FixtureClient) EnsureAdmin(email, password string, bootstrap bool) error {
	if !bootstrap {
		return c.SignIn(email, password)
	}

	var reply struct {
		Token string `json:"token"`
	}
	err := c.call(http.MethodPost, "/api/v1/auth/register",
		map[string]string{"email": email, "password": password},
		http.StatusCreated, &reply)
	if err != nil {
		return fmt.Errorf("sign in as the first account %s: %w", email, err)
	}
	if reply.Token == "" {
		return fmt.Errorf("sign in as the first account %s: the server returned no session", email)
	}
	c.token = reply.Token
	return nil
}

// SignIn opens the administrator session the rest of the build needs. Creating a
// customer, a site or an enrollment token is administrator work, so a client
// that has not signed in can build nothing at all.
func (c *FixtureClient) SignIn(email, password string) error {
	var reply struct {
		Token string `json:"token"`
	}
	err := c.call(http.MethodPost, "/api/v1/auth/login",
		map[string]string{"email": email, "password": password},
		http.StatusOK, &reply)
	if err != nil {
		return fmt.Errorf("sign in as %s: %w", email, err)
	}
	if reply.Token == "" {
		return fmt.Errorf("sign in as %s: the server returned no session", email)
	}
	c.token = reply.Token
	return nil
}

// BuildFixture walks a plan through the server and returns what now exists.
func (c *FixtureClient) BuildFixture(plan FixturePlan) (BuiltFixture, error) {
	if c.token == "" {
		return BuiltFixture{}, errors.New("build fixture: sign in first — creating a customer is administrator work")
	}

	built := BuiltFixture{
		Size:           plan.Size,
		Seed:           plan.Seed,
		PlannedDevices: plan.Devices,
	}

	for _, customer := range plan.Customers {
		created, err := c.createCustomer(customer)
		if err != nil {
			return BuiltFixture{}, err
		}
		built.Customers = append(built.Customers, created)
		built.Sites += len(created.SiteIDs)
	}

	for _, user := range plan.Users {
		if err := c.registerMember(user.Email); err != nil {
			return BuiltFixture{}, err
		}
		built.Users = append(built.Users, user.Email)
	}

	token, err := c.mintEnrollmentToken(plan)
	if err != nil {
		return BuiltFixture{}, err
	}
	built.EnrollmentToken = token

	return built, nil
}

// createCustomer creates one customer and the buildings its machines sit in.
func (c *FixtureClient) createCustomer(plan CustomerPlan) (BuiltCustomer, error) {
	var organization struct {
		ID string `json:"id"`
	}
	err := c.call(http.MethodPost, "/api/v1/organizations",
		map[string]string{"name": plan.Name}, http.StatusCreated, &organization)
	if err != nil {
		return BuiltCustomer{}, fmt.Errorf("create customer %s: %w", plan.Name, err)
	}

	built := BuiltCustomer{ID: organization.ID, Name: plan.Name, Devices: plan.Devices}
	for i := 0; i < plan.Sites; i++ {
		name := fmt.Sprintf("%s-site-%03d", plan.Name, i+1)
		var site struct {
			ID string `json:"id"`
		}
		err := c.call(http.MethodPost, "/api/v1/sites",
			map[string]string{"name": name, "organization_id": organization.ID},
			http.StatusCreated, &site)
		if err != nil {
			return BuiltCustomer{}, fmt.Errorf("create site %s: %w", name, err)
		}
		built.SiteIDs = append(built.SiteIDs, site.ID)
	}
	return built, nil
}

// registerMember creates one operator account. It registers rather than being
// created by an administrator, because that is the path a real person takes.
func (c *FixtureClient) registerMember(email string) error {
	var reply struct {
		Token string `json:"token"`
	}
	err := c.call(http.MethodPost, "/api/v1/auth/register",
		map[string]string{"email": email, "password": fixturePassword},
		http.StatusCreated, &reply)
	if err != nil {
		return fmt.Errorf("register operator %s: %w", email, err)
	}
	return nil
}

// mintEnrollmentToken issues the credential the fleet enrols with. It is the one
// an installer spends, it lives no longer than the run that spends it, and the
// run deletes it.
func (c *FixtureClient) mintEnrollmentToken(plan FixturePlan) (string, error) {
	var reply struct {
		Token string `json:"token"`
	}
	body := map[string]any{
		"label": fmt.Sprintf("%s-fixture-%d", loadTestMarker, plan.Seed),
		// Zero is unlimited, which is what a fleet of this size needs from one
		// credential.
		"max_uses":         0,
		"expires_in_hours": enrollmentTokenHours(c.runFor),
	}
	if err := c.call(http.MethodPost, "/api/v1/enrollment-tokens", body, http.StatusCreated, &reply); err != nil {
		return "", fmt.Errorf("mint enrollment token: %w", err)
	}
	if reply.Token == "" {
		return "", errors.New("mint enrollment token: the server returned no token")
	}
	return reply.Token, nil
}

// enrollmentTokenHours is how long the credential the fleet enrols with has to
// live.
//
// Every machine a phase starts calls the enrolment endpoint, so the credential
// has to outlive the whole walk rather than its first minute. An hour was the
// figure, which refused every arrival a profile made past its first sixty
// minutes — a five-hour soak would have enrolled nobody after the first phase
// and reported it as machines that failed to connect.
//
// The endpoint takes whole hours, so a part hour is rounded up: a run cut off a
// minute inside its last hour is the same defect in miniature. A whole extra
// hour is added on top of that for the fixture build, which happens before the
// clock starts and takes as long as the fleet is large. A run that declares no
// length is the everyday flat one, whose machines all arrive at the start, and
// it keeps the hour it has always had.
//
// A longer-lived credential is a credential worth more to anyone who takes it,
// and the answer is that this one is deleted by the run's own cleanup rather
// than left to expire.
func enrollmentTokenHours(runFor time.Duration) int {
	hours := 1
	if runFor > 0 {
		hours = int((runFor + time.Hour - 1) / time.Hour)
		hours++
	}
	return hours
}

// FileDevices puts each machine under the customer that is to hold it and into
// one of that customer's buildings, in the proportions the plan declared. An
// evenly spread fleet never asks the question a customer-scoped page is actually
// asked: the page that is slow in the field belongs to the customer holding most
// of the estate.
func (c *FixtureClient) FileDevices(built BuiltFixture, deviceIDs []string) error {
	for i, deviceID := range deviceIDs {
		if err := c.FileDevice(built, i, len(deviceIDs), deviceID); err != nil {
			return err
		}
	}
	return nil
}

// FileDevice files one machine, at its own place in the estate.
//
// It is a single machine rather than a pass over the fleet because a machine can
// only be filed once its row exists, and the row exists when it registers — so
// filing follows each arrival rather than waiting for the last one. Where in the
// estate this machine sits is what decides its customer, so the caller passes
// the position and the size rather than a list.
//
// Both halves go through the same interface a technician uses, and both are
// needed. The customer is who the machine answers under; the building is what
// every list narrows by, so a machine filed under a customer and into no
// building is one no scoped page can find.
func (c *FixtureClient) FileDevice(built BuiltFixture, index, total int, deviceID string) error {
	if len(built.Customers) == 0 {
		return errors.New("file machines: the fixture has no customers to file them under")
	}

	customer := built.Customers[c.customerFor(built, index, total)]
	path := fmt.Sprintf("/api/v1/devices/%s/organization", deviceID)
	body := map[string]string{"organization_id": customer.ID}
	if err := c.call(http.MethodPut, path, body, http.StatusOK, nil); err != nil {
		return fmt.Errorf("file machine %s under %s: %w", deviceID, customer.Name, err)
	}

	// A customer the plan gave no building to has nowhere to put this machine,
	// and leaving it under the customer alone is the honest state rather than a
	// failure. The planner gives every customer at least one, so this is a guard
	// against a fixture nobody has built rather than a case in play.
	if len(customer.SiteIDs) == 0 {
		return nil
	}

	// Spread by the machine's place in the estate, so a customer with thirty
	// buildings holds machines in thirty of them. Filing every one of a
	// customer's machines into its first building files the estate truthfully
	// and still leaves every other building empty — which is the same empty read
	// a scoped page gets today, wearing a different shape.
	site := customer.SiteIDs[index%len(customer.SiteIDs)]
	if err := c.call(http.MethodPatch, "/api/v1/devices/"+deviceID,
		map[string]string{"site_id": site}, http.StatusOK, nil); err != nil {
		return fmt.Errorf("file machine %s into a building of %s: %w", deviceID, customer.Name, err)
	}
	return nil
}

// customerFor picks which customer holds the machine at this position, so the
// fleet lands in the declared proportions rather than evenly.
func (c *FixtureClient) customerFor(built BuiltFixture, index, total int) int {
	planned := 0
	for _, customer := range built.Customers {
		planned += customer.Devices
	}
	if planned <= 0 || total <= 0 {
		return index % len(built.Customers)
	}

	// Scale each customer's share to however many machines actually arrived, so
	// a short fleet keeps the shape rather than filling the first customer.
	position := index * planned / total
	running := 0
	for i, customer := range built.Customers {
		running += customer.Devices
		if position < running {
			return i
		}
	}
	return len(built.Customers) - 1
}

// call makes one request and decodes its reply, treating any other status as the
// failure it is. A fixture built on top of a refused call is a fleet nobody
// declared, and the numbers measured against it look ordinary.
func (c *FixtureClient) call(method, path string, body any, wantStatus int, out any) error {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), fixtureRequestTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}

	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer response.Body.Close()

	if response.StatusCode != wantStatus {
		return fmt.Errorf("%s %s: server answered %d, expected %d", method, path, response.StatusCode, wantStatus)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(response.Body).Decode(out); err != nil {
		return fmt.Errorf("%s %s: decode reply: %w", method, path, err)
	}
	return nil
}
