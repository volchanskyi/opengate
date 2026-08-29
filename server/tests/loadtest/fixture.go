package main

import (
	"fmt"
	"math"
	"sort"
)

// The fixture is what "the same run twice" means. Two runs against different
// fleets produce different numbers for reasons nobody can separate afterwards,
// so a fleet is derived from a size and a seed and from nothing else — no
// clock, no counter, no leftovers from the run before.
//
// The three sizes anchor on the committed reference fleet. The third is not a
// fourth size but a different distribution of the second, because an evenly
// spread fleet never asks the question a tenant-scoped read is actually asked:
// the page that is slow in the field is the one belonging to the customer who
// holds most of the estate.

// loadTestMarker appears in every name a run creates. Cleanup that cannot
// recognise what a run made cannot remove it, and an environment whose every
// user is load-test residue got there through exactly that gap.
const loadTestMarker = "opengate-loadtest"

// referenceDevices is the committed reference fleet the sizes anchor on.
const referenceDevices = 500

// largeMultiple is how much bigger the large fleet is than the reference.
const largeMultiple = 4

// lopsidedMajorityShare is how much of the fleet the biggest customer holds in
// the lopsided distribution.
const lopsidedMajorityShare = 0.8

// devicesPerSite is the fleet's shape inside a customer: a site is a building,
// and a building with two machines and one with ten thousand are different
// products.
const devicesPerSite = 50

// CustomerPlan is one customer's share of the fleet.
type CustomerPlan struct {
	Name    string `json:"name"`
	Sites   int    `json:"sites"`
	Devices int    `json:"devices"`
}

// UserPlan is one operator identity the run drives the API as.
type UserPlan struct {
	Email string `json:"email"`
	Admin bool   `json:"admin"`
}

// CleanupManifest is what a run must remove afterwards. It is produced with the
// plan rather than reconstructed from what is found later, so what to remove is
// known before anything is created — including when the run dies halfway.
type CleanupManifest struct {
	Marker  string   `json:"marker"`
	Tenant  string   `json:"tenant"`
	Users   []string `json:"users"`
	Devices int      `json:"devices"`
}

// FixturePlan is a whole fleet, decided before anything is created.
type FixturePlan struct {
	Size FixtureSize `json:"size"`
	Seed uint64      `json:"seed"`

	// TenantName is the run's own tenant. Load-test identities never live in
	// the default tenant: a user created there is a user in the fleet everybody
	// else reads.
	TenantName string `json:"tenant_name"`

	Customers []CustomerPlan `json:"customers"`
	Users     []UserPlan     `json:"users"`

	Sites   int `json:"sites"`
	Devices int `json:"devices"`
}

// RunsInsideTimedPhase is always false. Building a fleet is thousands of writes
// and it would be the largest thing in any phase it shared, so it happens
// before the clock starts. Stating it here keeps it from being re-argued at
// each call site.
func (p FixturePlan) RunsInsideTimedPhase() bool { return false }

// CleanupManifest is what this plan obliges the run to remove.
func (p FixturePlan) CleanupManifest() CleanupManifest {
	emails := make([]string, len(p.Users))
	for i, user := range p.Users {
		emails[i] = user.Email
	}
	return CleanupManifest{
		Marker:  loadTestMarker,
		Tenant:  p.TenantName,
		Users:   emails,
		Devices: p.Devices,
	}
}

// PlanFixture decides a whole fleet from a size and a seed.
func PlanFixture(size FixtureSize, seed uint64) (FixturePlan, error) {
	devices, err := fixtureDeviceCount(size)
	if err != nil {
		return FixturePlan{}, err
	}

	plan := FixturePlan{
		Size:       size,
		Seed:       seed,
		TenantName: fmt.Sprintf("%s-tenant", loadTestMarker),
		Devices:    devices,
	}

	// One source of variation, seeded once: every varying decision below draws
	// from it in a fixed order, which is what makes the same seed reproduce the
	// same fleet exactly rather than approximately.
	source := &sequence{state: seed}

	plan.Customers = planCustomers(size, devices, seed, source)
	for _, customer := range plan.Customers {
		plan.Sites += customer.Sites
	}
	plan.Users = planUsers(size, seed)

	return plan, nil
}

func fixtureDeviceCount(size FixtureSize) (int, error) {
	switch size {
	case FixtureSmall:
		return referenceDevices, nil
	case FixtureLarge, FixtureLopsided:
		return referenceDevices * largeMultiple, nil
	default:
		return 0, fmt.Errorf("unknown fixture size %q; the sizes are %v", size, fixtureSizes)
	}
}

// planCustomers splits the fleet between customers, naming each after the run
// it belongs to. The lopsided distribution gives one customer most of it; the
// others spread the fleet evenly, which is the shape that flatters a
// tenant-scoped read.
func planCustomers(size FixtureSize, devices int, seed uint64, source *sequence) []CustomerPlan {
	shares := customerShares(size, devices, source)

	customers := make([]CustomerPlan, 0, len(shares))
	for i, share := range shares {
		customers = append(customers, CustomerPlan{
			// The seed is in the name for the reason it is in every account
			// address: a customer's name is unique inside its tenant, so a name
			// built from the marker alone is the same name every night. The
			// second night is then refused the customer it asked for and the
			// fleet it was building never connects.
			Name:    fmt.Sprintf("%s-%d-customer-%02d", loadTestMarker, seed, i+1),
			Devices: share,
			Sites:   sitesFor(share),
		})
	}
	// Biggest first, so a reader sees the shape of the fleet in the first row
	// rather than having to sort it themselves.
	sort.SliceStable(customers, func(i, j int) bool { return customers[i].Devices > customers[j].Devices })
	return customers
}

// customerShares splits `devices` into per-customer counts that sum to it
// exactly. The remainder is given to the first customer rather than dropped,
// because a fleet that does not add up is a fleet nobody declared.
func customerShares(size FixtureSize, devices int, source *sequence) []int {
	if size == FixtureLopsided {
		majority := int(float64(devices) * lopsidedMajorityShare)
		rest := spreadEvenly(devices-majority, 4, source)
		return append([]int{majority}, rest...)
	}
	// Between five and eight customers, so the fleet's shape varies with the
	// seed while its size does not.
	return spreadEvenly(devices, 5+source.below(4), source)
}

// spreadEvenly splits total between n buckets, jittering each by up to a fifth
// so no two seeds produce the same fleet, and giving the remainder to the first
// bucket so the parts sum to the whole.
func spreadEvenly(total, n int, source *sequence) []int {
	if n < 1 {
		n = 1
	}
	base := total / n
	shares := make([]int, n)
	assigned := 0
	for i := 1; i < n; i++ {
		jitter := 0
		if base > 5 {
			jitter = source.below(base/5) - base/10
		}
		share := base + jitter
		if share < 1 {
			share = 1
		}
		shares[i] = share
		assigned += share
	}
	shares[0] = total - assigned
	if shares[0] < 1 {
		shares[0] = 1
	}
	return shares
}

// sequence is a small, fixed, reproducible number sequence.
//
// It is written out rather than taken from a library because what is needed
// here is reproducibility, not randomness: the same seed must produce the same
// fleet on any machine and in any Go release, and a standard generator's
// algorithm is free to change between versions. Nothing here protects anything,
// so this must never be used where unpredictability matters.
type sequence struct {
	state uint64
}

// next advances the sequence. This is splitmix64, whose constants are its
// definition; it is chosen for being short enough to read and stable forever.
func (s *sequence) next() uint64 {
	s.state += 0x9e3779b97f4a7c15
	z := s.state
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

// below returns the next value in [0, n), or 0 when n is not positive.
func (s *sequence) below(n int) int {
	if n <= 0 {
		return 0
	}
	return safeInt(s.next() % uint64(n))
}

// safeInt narrows a value the caller has already bounded, clamping anything
// out of range so the conversion cannot overflow (gosec G115).
func safeInt(v uint64) int {
	if v > math.MaxInt {
		return math.MaxInt
	}
	return int(v)
}

// sitesFor is how many buildings a customer's machines are spread across, never
// fewer than one.
func sitesFor(devices int) int {
	sites := devices / devicesPerSite
	if sites < 1 {
		return 1
	}
	return sites
}

// planUsers builds the operator identities. The count follows the fleet: an
// estate four times the size is looked after by more people, and a run with one
// operator never contends on anything a real one contends on.
func planUsers(size FixtureSize, seed uint64) []UserPlan {
	count := 5
	if size != FixtureSmall {
		count = 20
	}

	users := make([]UserPlan, count)
	for i := range users {
		users[i] = UserPlan{
			Email: fmt.Sprintf("%s-%d-user-%02d@%s.invalid", loadTestMarker, seed, i+1, loadTestMarker),
			Admin: i == 0,
		}
	}
	return users
}
