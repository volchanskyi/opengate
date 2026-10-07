package device

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEveryDeviceReadSharesTheProjectionAndTheTenantClause(t *testing.T) {
	statements := map[string]string{
		"get":                           getDeviceQuery,
		"get by AMT UUID":               getDeviceByAMTUUIDQuery,
		"list":                          listDevicesQuery,
		"list by site":                  listDevicesBySiteQuery,
		"list by organization":          listDevicesByOrganizationQuery,
		"list by site and organization": listDevicesBySiteAndOrganizationQuery,
	}
	for name, statement := range statements {
		t.Run(name, func(t *testing.T) {
			assert.True(t, strings.HasPrefix(statement, deviceSelect),
				"the statement must select exactly the columns scanDevice reads")
			assert.Contains(t, statement, "WHERE d.tenant_id = current_setting('app.current_tenant')::uuid")
		})
	}
}
