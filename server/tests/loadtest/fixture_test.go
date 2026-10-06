package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

func TestTheSeedDecidesTheFleetAndNothingElseDoes(t *testing.T) {
	assert.Equal(t, planned(t, FixtureLopsided, 42), planned(t, FixtureLopsided, 42))

	first, second := planned(t, FixtureLarge, 1), planned(t, FixtureLarge, 2)
	assert.NotEqual(t, first.Customers, second.Customers)
	assert.Equal(t, first.Devices, second.Devices, "the seed varies the distribution, never the size")
}

func TestEveryNameCarriesTheLoadTestMarker(t *testing.T) {
	plan := planned(t, FixtureSmall, 1)

	for _, customer := range plan.Customers {
		assert.Contains(t, customer.Name, loadTestMarker)
	}
	for _, user := range plan.Users {
		assert.Contains(t, user.Email, loadTestMarker)
	}
}

func TestAnUnknownFixtureSizeIsRefused(t *testing.T) {
	_, err := PlanFixture("enormous", 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "enormous")
}

func TestTwoRunsNeverAskForTheSameCustomer(t *testing.T) {
	first, second := planned(t, FixtureSmall, 1), planned(t, FixtureSmall, 2)

	taken := map[string]bool{}
	for _, customer := range first.Customers {
		taken[customer.Name] = true
	}
	for _, customer := range second.Customers {
		assert.False(t, taken[customer.Name],
			"customer %q is asked for by both runs, so the second is refused", customer.Name)
	}
}

func TestACustomerNameCarriesTheRunItBelongsTo(t *testing.T) {
	plan := planned(t, FixtureSmall, 4242)
	for _, customer := range plan.Customers {
		assert.Contains(t, customer.Name, "4242")
		assert.Contains(t, customer.Name, loadTestMarker)
	}
}
