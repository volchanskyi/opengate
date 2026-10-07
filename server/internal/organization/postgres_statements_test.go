package organization

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEveryStatementCarriesTheTenantClause(t *testing.T) {
	statements := map[string]string{
		"get by id":    getByIDQuery,
		"list active":  listActiveQuery,
		"list all":     listAllQuery,
		"rename":       renameQuery,
		"set archived": setArchivedQuery,
		"delete":       deleteQuery,
		"oldest":       oldestQuery,
		"find by name": findByNameQuery,
	}
	for name, statement := range statements {
		t.Run(name, func(t *testing.T) {
			assert.Contains(t, statement, "WHERE tenant_id = current_setting('app.current_tenant')::uuid")
		})
	}
}
