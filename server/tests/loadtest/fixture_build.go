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

// fixtureRequestTimeout bounds one call to the public API.
const fixtureRequestTimeout = 30 * time.Second

// fixturePassword is the fixed password of every account a run creates.
const fixturePassword = "LoadTestPass123!"

// FixtureClient drives the public API as one signed-in administrator.
type FixtureClient struct {
	baseURL string
	http    *http.Client
	token   string
	// runFor is how long the run lasts, which the enrollment credential has to outlive.
	runFor time.Duration
}

// NewFixtureClient builds a client against one server, for a run of no declared length.
func NewFixtureClient(baseURL string) *FixtureClient {
	return NewFixtureClientForRun(baseURL, 0)
}

// NewFixtureClientForRun builds a client for a run of a known length, which the fleet's
// enrollment credential outlives.
func NewFixtureClientForRun(baseURL string, runFor time.Duration) *FixtureClient {
	return &FixtureClient{
		baseURL: baseURL,
		http:    &http.Client{Timeout: fixtureRequestTimeout},
		runFor:  runFor,
	}
}

// Token is the session this client holds, empty before it has signed in.
func (c *FixtureClient) Token() string { return c.token }

// BuiltCustomer is one customer on the server, with its sites and planned share of the fleet.
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
	// PlannedDevices is how many machines the fleet is to hold; they arrive by enrolling.
	PlannedDevices int `json:"planned_devices"`
	// EnrollmentToken is the short-lived credential those machines spend.
	EnrollmentToken string `json:"-"`
}

// Counts is this fixture in the shape a bundle records it; its device count is the planned one.
func (b BuiltFixture) Counts() FixtureCounts {
	return FixtureCounts{
		Size: b.Size,
		// Load identities live in the default tenant.
		Tenants:        1,
		Customers:      len(b.Customers),
		Sites:          b.Sites,
		Users:          len(b.Users),
		PlannedDevices: b.PlannedDevices,
	}
}

// EnsureAdmin opens the administrator session by signing in, or with bootstrap by registering
// the first account, which the server promotes to administrator.
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

// SignIn opens the administrator session the rest of the build needs.
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

// registerMember creates one operator account through the public registration endpoint.
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

// mintEnrollmentToken issues the credential the fleet enrols with, which the run deletes.
func (c *FixtureClient) mintEnrollmentToken(plan FixturePlan) (string, error) {
	var reply struct {
		Token string `json:"token"`
	}
	body := map[string]any{
		"label": fmt.Sprintf("%s-fixture-%d", loadTestMarker, plan.Seed),
		// Zero uses are unlimited.
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

// enrollmentTokenHours is the credential lifetime in whole hours: the run length rounded up
// plus one hour for the fixture build, or one hour for a run of no declared length.
func enrollmentTokenHours(runFor time.Duration) int {
	hours := 1
	if runFor > 0 {
		hours = int((runFor + time.Hour - 1) / time.Hour)
		hours++
	}
	return hours
}

// FileDevices files each machine under a customer and into one of its sites, in the
// proportions the plan declared.
func (c *FixtureClient) FileDevices(built BuiltFixture, deviceIDs []string) error {
	for i, deviceID := range deviceIDs {
		if err := c.FileDevice(built, i, len(deviceIDs), deviceID); err != nil {
			return err
		}
	}
	return nil
}

// FileDevice files one machine at its index in an estate of total, under a customer
// and into one of that customer's sites.
func (c *FixtureClient) FileDevice(built BuiltFixture, index, total int, deviceID string) error {
	if len(built.Customers) == 0 {
		return errors.New("file machines: the fixture has no customers to file them under")
	}

	// Filing presents the machine's own address, so it spends that address's arrival allowance.
	presented := presentedAddress(index)

	customer := built.Customers[c.customerFor(built, index, total)]
	path := fmt.Sprintf("/api/v1/devices/%s/organization", deviceID)
	body := map[string]string{"organization_id": customer.ID}
	// Both halves wait for the machine's row, which the register frame writes.
	if err := waitForTheRow(func() error {
		return c.callAs(presented, http.MethodPut, path, body, http.StatusOK, nil)
	}); err != nil {
		return fmt.Errorf("file machine %s under %s: %w", deviceID, customer.Name, err)
	}

	// A customer with no site keeps the machine under the customer alone.
	if len(customer.SiteIDs) == 0 {
		return nil
	}

	// The machine's index picks the site, spreading a customer's machines over all its sites.
	site := customer.SiteIDs[index%len(customer.SiteIDs)]
	if err := waitForTheRow(func() error {
		return c.callAs(presented, http.MethodPatch, "/api/v1/devices/"+deviceID,
			map[string]string{"site_id": site}, http.StatusOK, nil)
	}); err != nil {
		return fmt.Errorf("file machine %s into a building of %s: %w", deviceID, customer.Name, err)
	}
	return nil
}

// customerFor picks the customer holding the machine at this position, in the declared proportions.
func (c *FixtureClient) customerFor(built BuiltFixture, index, total int) int {
	planned := 0
	for _, customer := range built.Customers {
		planned += customer.Devices
	}
	if planned <= 0 || total <= 0 {
		return index % len(built.Customers)
	}

	// The position is scaled to the machines that arrived, so a short fleet keeps its shape.
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

// call makes one request and decodes its reply, failing on any status other than wantStatus.
func (c *FixtureClient) call(method, path string, body any, wantStatus int, out any) error {
	return c.callAs("", method, path, body, wantStatus, out)
}

// callAs is call, presenting an address; the administrator calls present none and the
// per-machine calls present the machine's own.
func (c *FixtureClient) callAs(presented, method, path string, body any, wantStatus int, out any) error {
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
	presentAddress(request, presented)
	if c.token != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}

	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer response.Body.Close()

	if response.StatusCode != wantStatus {
		return &apiRefusal{
			Method: method, Path: path,
			Status: response.StatusCode, Want: wantStatus,
			Detail: detailOf(response.Body),
		}
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(response.Body).Decode(out); err != nil {
		return fmt.Errorf("%s %s: decode reply: %w", method, path, err)
	}
	return nil
}
