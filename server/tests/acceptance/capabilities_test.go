package acceptance

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// productChapters is where the capabilities live. The gate reads the
// directory rather than a list of its own: a thirteenth chapter must fail
// until somebody states its outcome, and a hand-kept list would silently rot
// instead.
const productChapters = "../../../docs/product"

// capabilityOutcomes binds every chapter of the product documentation to the
// outcomes that prove it. A chapter with no outcome is a promise nothing
// keeps; an outcome naming no chapter is a test nobody can place.
//
// This table is the whole map, and the guard below checks it in both
// directions against the directory and against this package's own syntax tree,
// so it cannot drift from either.
var capabilityOutcomes = map[string][]string{
	"Agent-Deployment.md": {
		"TestAMachineEnrolsWithATokenAndAppearsOnline",
		"TestAnEnrolmentTokenUsedTwiceGivesTwoDistinctMachines",
		"TestAnExhaustedEnrolmentTokenIsRefused",
		"TestAnUnknownEnrolmentTokenIsRefused",
		"TestAMachineRebuiltWithANewCertificateIsTheSameMachine",
	},
	"Fleet-and-Devices.md": {
		"TestTheDashboardAgreesWithTheDeviceList",
		"TestATechnicianSeesOneCustomersMachinesAtATime",
	},
	"Remote-Sessions.md": {
		"TestATechnicianOpensATerminalAndTheMachineIsToldToStartIt",
		"TestASessionForAMachineThatIsOfflineIsRefusedWithAReason",
		"TestASessionOnAMachineThatDisappearsStopsBeingUsable",
		"TestACustomerFilterNarrowsAndDoesNotPermit",
	},
	"Device-Health.md": {
		"TestAMachineReportsAMinuteAndTheTechnicianReadsItBack",
		"TestReadingsThatArriveWithNothingInThemAreAccountedFor",
		"TestAReadingFromAMachineWithAWrongClockIsStillKept",
		"TestADimensionTheFleetNeverAgreedToIsRefused",
	},
	"Alerts-and-Rules.md": {
		"TestARuleReachesAMachineAndItsBreachComesBackAsAnAlert",
	},
	"Rule-Administration.md": {
		"TestATunedThresholdReachesOneCustomerAndNotTheOther",
		"TestAStopSwitchReachesMachinesAlreadyCarryingTheRule",
	},
	"Investigations.md": {
		"TestAnAlertBecomesAnIncidentATechnicianClosesWithACause",
		"TestAnIncidentIdFromAnotherTenantIsIndistinguishableFromAMissingOne",
	},
	"Endpoint-Logs.md": {
		"TestATechnicianPullsALogAndTheSecretInItNeverReachesThem",
		"TestATechnicianWithoutElevatedPermissionCannotPullALog",
	},
	"Intel-AMT.md": {
		"TestATechnicianPowersOnAnUnresponsiveMachine",
		"TestPoweringOnAMachineWhoseControllerIsSilentSaysSo",
		"TestPoweringOnAMachineInAnotherTenantIsNotFound",
	},
	"Agent-Updates.md": {
		"TestAnAdministratorPublishesABuildAndTheMachineAcknowledgesIt",
		"TestABuildIsNotPushedToAMachineOfAnotherShape",
	},
	"Tenancy-and-Access.md": {
		"TestOneTenantsEstateIsInvisibleToAnother",
		"TestTheLastAdministratorCannotBeDemoted",
	},
	"Data-Erasure.md": {
		"TestDeletingAMachineRemovesItAndStopsTrustingItsAgent",
		"TestDeletingAMachineFromAnotherTenantIsNotFound",
	},
}

// guardsOfTheMapItself are the tests below, which prove the binding rather
// than a capability. They are the only functions in this package exempt from
// naming a chapter, and they are named here rather than matched by a prefix so
// the exemption cannot quietly grow.
var guardsOfTheMapItself = map[string]bool{
	"TestEveryCapabilityHasAnOutcome":    true,
	"TestEveryOutcomeNamesOneCapability": true,
}

// TestEveryCapabilityHasAnOutcome reads the chapter directory and insists each
// chapter is proven. Adding a capability to the product without stating what a
// customer gets from it fails here, naming the chapter — which is the point:
// the alternative is noticing months later that nothing ever tested it.
func TestEveryCapabilityHasAnOutcome(t *testing.T) {
	t.Parallel()

	chapters, err := filepath.Glob(filepath.Join(productChapters, "*.md"))
	require.NoError(t, err)
	require.NotEmpty(t, chapters, "the product documentation must be readable from here")

	declared := declaredTests(t)

	for _, path := range chapters {
		chapter := filepath.Base(path)
		outcomes, bound := capabilityOutcomes[chapter]
		require.Truef(t, bound, "%s is a capability with no outcome test — state what a customer gets from it", chapter)
		require.NotEmptyf(t, outcomes, "%s names no outcome", chapter)

		for _, outcome := range outcomes {
			assert.Truef(t, declared[outcome],
				"%s names %s, which is not a test in this package", chapter, outcome)
		}
	}
}

// TestEveryOutcomeNamesOneCapability is the other direction. An outcome that
// names no chapter is a test nobody can place when it goes red, and a chapter
// name that does not exist is a binding to nothing.
func TestEveryOutcomeNamesOneCapability(t *testing.T) {
	t.Parallel()

	claimed := map[string][]string{}
	for chapter, outcomes := range capabilityOutcomes {
		_, err := os.Stat(filepath.Join(productChapters, chapter))
		assert.NoErrorf(t, err, "%s is bound to outcomes but is not a chapter", chapter)
		for _, outcome := range outcomes {
			claimed[outcome] = append(claimed[outcome], chapter)
		}
	}

	for outcome, chapters := range claimed {
		assert.Lenf(t, chapters, 1, "%s names %d chapters; an outcome proves exactly one",
			outcome, len(chapters))
	}

	var unplaced []string
	for name := range declaredTests(t) {
		if guardsOfTheMapItself[name] || len(claimed[name]) > 0 {
			continue
		}
		unplaced = append(unplaced, name)
	}
	sort.Strings(unplaced)
	assert.Emptyf(t, unplaced, "these outcomes name no capability: %s", strings.Join(unplaced, ", "))
}

// declaredTests reads this package's own syntax tree and returns every test
// function in it. Reading the source rather than a list is what makes the
// binding impossible to satisfy by forgetting.
func declaredTests(t *testing.T) map[string]bool {
	t.Helper()

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", nil, 0)
	require.NoError(t, err)

	declared := map[string]bool{}
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				fn, isFunc := decl.(*ast.FuncDecl)
				if !isFunc || fn.Recv != nil || !strings.HasPrefix(fn.Name.Name, "Test") {
					continue
				}
				declared[fn.Name.Name] = true
			}
		}
	}
	require.NotEmpty(t, declared, "the package must be able to read its own tests")
	return declared
}
