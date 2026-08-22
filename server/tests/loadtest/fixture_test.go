package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A fixture is what "the same run twice" means. If the fleet differs between
// two runs, so does every number, and nobody can say which difference caused
// which. So the plan is derived from the size and a seed, and nothing else.

// planned builds one fleet, failing the test rather than returning an error the
// case would have to handle.
func planned(t *testing.T, size FixtureSize, seed uint64) FixturePlan {
	t.Helper()
	plan, err := PlanFixture(size, seed)
	require.NoError(t, err)
	return plan
}

func TestTheThreeSizesAreTheCommittedReferenceAndItsMultiples(t *testing.T) {
	assert.Equal(t, 500, planned(t, FixtureSmall, 1).Devices,
		"the small fixture is the committed 500-device reference fleet")
	assert.Equal(t, 2000, planned(t, FixtureLarge, 1).Devices)
	assert.Equal(t, 2000, planned(t, FixtureLopsided, 1).Devices,
		"the lopsided fixture holds the same fleet as the large one, distributed differently")
}

// The lopsided fixture exists because an evenly spread fleet never asks the
// question a tenant-scoped read is actually asked: one customer holding most of
// the estate is the page that is slow in the field.
func TestTheDistributionIsWhatTheLopsidedFixtureVaries(t *testing.T) {
	lopsided := planned(t, FixtureLopsided, 1)
	require.NotEmpty(t, lopsided.Customers)
	assert.Greater(t, float64(lopsided.Customers[0].Devices), 0.7*float64(lopsided.Devices),
		"the lopsided fixture's largest customer must hold most of the fleet")

	even := planned(t, FixtureLarge, 1)
	require.Greater(t, len(even.Customers), 1)
	for _, customer := range even.Customers {
		assert.Less(t, float64(customer.Devices), 0.5*float64(even.Devices))
	}
}

// Every plan adds up. A fixture whose parts do not sum to its whole seeds a
// fleet nobody declared.
func TestEveryPlanAddsUp(t *testing.T) {
	for _, size := range FixtureSizes() {
		t.Run(string(size), func(t *testing.T) {
			plan := planned(t, size, 7)

			devices, sites := 0, 0
			for _, customer := range plan.Customers {
				assert.Positive(t, customer.Devices, "customer %s has no devices", customer.Name)
				assert.Positive(t, customer.Sites, "customer %s has no sites", customer.Name)
				devices += customer.Devices
				sites += customer.Sites
			}
			assert.Equal(t, plan.Devices, devices)
			assert.Equal(t, plan.Sites, sites)
		})
	}
}

// Same size, same seed, same fleet — down to the names, which is what makes a
// run reproducible rather than merely similar. A different seed is a different
// fleet, or the seed does nothing and every run shares one shape.
func TestTheSeedDecidesTheFleetAndNothingElseDoes(t *testing.T) {
	assert.Equal(t, planned(t, FixtureLopsided, 42), planned(t, FixtureLopsided, 42))

	first, second := planned(t, FixtureLarge, 1), planned(t, FixtureLarge, 2)
	assert.NotEqual(t, first.Customers, second.Customers)
	assert.Equal(t, first.Devices, second.Devices, "the seed varies the distribution, never the size")
}

// Names carry the run's own marker, because a cleanup that cannot recognise
// what a run created cannot remove it — and an environment whose every user is
// residue got there by exactly that gap. The tenant is never the default one: a
// user created there is a user in the fleet everybody else reads.
func TestEveryNameCarriesTheLoadTestMarker(t *testing.T) {
	plan := planned(t, FixtureSmall, 1)

	assert.Contains(t, plan.TenantName, loadTestMarker)
	assert.NotEqual(t, "default", plan.TenantName)
	for _, customer := range plan.Customers {
		assert.Contains(t, customer.Name, loadTestMarker)
	}
	for _, user := range plan.Users {
		assert.Contains(t, user.Email, loadTestMarker)
	}
}

// The cleanup manifest is produced with the fixture rather than reconstructed
// afterwards, so what to remove is known before anything is created.
func TestTheFixturePlanCarriesItsOwnCleanupManifest(t *testing.T) {
	plan := planned(t, FixtureSmall, 1)

	manifest := plan.CleanupManifest()
	assert.Equal(t, plan.TenantName, manifest.Tenant)
	assert.Len(t, manifest.Users, len(plan.Users))
	assert.Equal(t, plan.Devices, manifest.Devices)
	assert.Equal(t, loadTestMarker, manifest.Marker)
}

func TestAnUnknownFixtureSizeIsRefused(t *testing.T) {
	_, err := PlanFixture("enormous", 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "enormous")
}

// A fixture is built outside every timed phase. Stating the ownership as a
// property of the plan keeps it from being re-litigated at each call site.
func TestAFixtureDeclaresItRunsOutsideTimedPhases(t *testing.T) {
	assert.False(t, planned(t, FixtureSmall, 1).RunsInsideTimedPhase())
}
