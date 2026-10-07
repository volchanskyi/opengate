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

// productChapters is the directory whose chapters each need an outcome.
const productChapters = "../../../docs/product"

// capabilityOutcomes binds every product chapter to the outcome tests that prove it.
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
		"TestASessionLeftByAMachineThatWentAwayIsReclaimed",
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
		"TestAFailureThatCrossesNoLineStillReachesTheQueue",
		"TestAFindingOutOfHistoryBelongsWhereItHappened",
	},
	"Rule-Administration.md": {
		"TestATunedThresholdReachesOneCustomerAndNotTheOther",
		"TestAStopSwitchReachesMachinesAlreadyCarryingTheRule",
		"TestARuleTheCustomerStoppedRaisesNothingInTheirQueue",
		"TestAStoppedRuleReachesAMachineThatNeverDisconnects",
		"TestARetunedThresholdReachesAMachineThatNeverDisconnects",
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

// guardsOfTheMapItself names the only tests exempt from naming a chapter.
var guardsOfTheMapItself = map[string]bool{
	"TestEveryCapabilityHasAnOutcome":    true,
	"TestEveryOutcomeNamesOneCapability": true,
}

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

// declaredTests returns every test function declared in this package's source.
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
