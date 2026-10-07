package main

import (
	"fmt"
	"math"
	"sort"
)

// loadTestMarker appears in every name a run creates, so cleanup can recognise what a run made.
const loadTestMarker = "opengate-loadtest"

// referenceDevices is the committed reference fleet the sizes anchor on.
const referenceDevices = 500

// largeMultiple is how much bigger the large fleet is than the reference.
const largeMultiple = 4

// lopsidedMajorityShare is how much of the fleet the biggest customer holds in
// the lopsided distribution.
const lopsidedMajorityShare = 0.8

// devicesPerSite is how many devices one site (a building) holds inside a customer.
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

// FixturePlan is a whole fleet, decided before anything is created.
type FixturePlan struct {
	Size FixtureSize `json:"size"`
	Seed uint64      `json:"seed"`

	Customers []CustomerPlan `json:"customers"`
	Users     []UserPlan     `json:"users"`

	Sites   int `json:"sites"`
	Devices int `json:"devices"`
}

// PlanFixture decides a whole fleet from a size and a seed.
func PlanFixture(size FixtureSize, seed uint64) (FixturePlan, error) {
	devices, err := fixtureDeviceCount(size)
	if err != nil {
		return FixturePlan{}, err
	}

	plan := FixturePlan{
		Size:    size,
		Seed:    seed,
		Devices: devices,
	}

	// Every varying decision draws from this one seeded source in a fixed order.
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

// planCustomers splits the fleet between customers, naming each after the run's seed.
func planCustomers(size FixtureSize, devices int, seed uint64, source *sequence) []CustomerPlan {
	shares := customerShares(size, devices, source)

	customers := make([]CustomerPlan, 0, len(shares))
	for i, share := range shares {
		customers = append(customers, CustomerPlan{
			// The seed keeps the name unique per run, since a customer name is unique within its tenant.
			Name:    fmt.Sprintf("%s-%d-customer-%02d", loadTestMarker, seed, i+1),
			Devices: share,
			Sites:   sitesFor(share),
		})
	}
	sort.SliceStable(customers, func(i, j int) bool { return customers[i].Devices > customers[j].Devices })
	return customers
}

// customerShares splits `devices` into per-customer counts that sum to it exactly, with
// the remainder going to the first customer.
func customerShares(size FixtureSize, devices int, source *sequence) []int {
	if size == FixtureLopsided {
		majority := int(float64(devices) * lopsidedMajorityShare)
		rest := spreadEvenly(devices-majority, 4, source)
		return append([]int{majority}, rest...)
	}
	// Five to eight customers, chosen by the seed.
	return spreadEvenly(devices, 5+source.below(4), source)
}

// spreadEvenly splits total between n buckets, jittering each by up to a tenth either way, and
// gives the remainder to the first bucket so the parts sum to the whole.
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

// sequence is a reproducible number sequence: one seed gives the same values in any Go
// release. It is predictable by design and unfit for anything security-sensitive.
type sequence struct {
	state uint64
}

// next advances the sequence with splitmix64.
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

// planUsers builds the operator identities, more of them for a larger fleet.
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
