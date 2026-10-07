package alerts

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

const (
	qHostname     = `SELECT hostname FROM devices WHERE id = $1`
	qCustomerName = `SELECT name FROM organizations WHERE id = $1`
	qRename       = `UPDATE users SET display_name = $2 WHERE id = $1`
	qDropDevice   = `DELETE FROM devices WHERE id = $1`
	qDropUser     = `DELETE FROM users WHERE id = $1`
)

// seedAbout seeds an open room about one host, site or customer of the customer under test.
func (e estate) seedAbout(t *testing.T, scope Scope, key uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	e.exec(t,
		`INSERT INTO incidents (id, tenant_id, organization_id, rule_id, scope, scope_key,
		                        severity, status, first_seen, last_seen)
		 VALUES ($1::uuid, $2::uuid, $3::uuid, 'disk-critical', $4::text, $5::uuid,
		         'warning', 'new', $6::timestamptz, $6::timestamptz)`,
		id, e.tenant, e.org, string(scope), key, e.now)
	return id
}

func (e estate) named(t *testing.T, query string, id uuid.UUID) string {
	t.Helper()
	var name string
	e.readOne(t, query, []any{id}, &name)
	return name
}

func scopeNames(page Page) map[uuid.UUID]string {
	out := make(map[uuid.UUID]string, len(page.Incidents))
	for _, incident := range page.Incidents {
		out[incident.ID] = incident.ScopeName
	}
	return out
}

func TestQueueAndRoomNameWhatEachRoomIsAbout(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	site := testutil.SeedSiteIn(t, e.ctx, e.store, e.org)
	leaving := testutil.SeedDeviceIn(t, e.ctx, e.store, e.org, site.ID)

	host := e.seedAbout(t, ScopeDevice, e.device)
	onSite := e.seedAbout(t, ScopeSite, site.ID)
	customer := e.seedAbout(t, ScopeOrganization, e.org)
	removed := e.seedAbout(t, ScopeDevice, leaving.ID)
	e.exec(t, qDropDevice, leaving.ID)

	want := map[uuid.UUID]string{
		host:     e.named(t, qHostname, e.device),
		onSite:   site.Name,
		customer: e.named(t, qCustomerName, e.org),
		removed:  "",
	}
	assert.Equal(t, want, scopeNames(e.queue(t, Filter{OrganizationID: e.org})))
	assert.Equal(t, want, scopeNames(e.queue(t, Filter{})), "every customer at once")

	for id, name := range want {
		assert.Equal(t, name, e.opened(t, id).Incident.ScopeName)
	}
}

func TestRoomNamesTheHostOfEachAlert(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	e.recordUnder(t, e.variant(nil), perCustomer, Stored)
	id := e.roomFor(t, perCustomer, "disk-critical", e.org).ID

	room := e.opened(t, id)

	require.Len(t, room.Alerts, 1)
	assert.Equal(t, e.named(t, qHostname, e.device), room.Alerts[0].Hostname)
}

func TestRoomNamesItsPeopleAndNobodyBeyondTheTenant(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	dispatcher := testutil.SeedUser(t, e.ctx, e.store)
	holder := testutil.SeedUser(t, e.ctx, e.store)
	departed := testutil.SeedUser(t, e.ctx, e.store)
	stranger := testutil.SeedUser(t, e.neighbour(t, "Northwind").ctx, e.store)
	e.exec(t, qRename, dispatcher.ID, "Sam Okafor")
	e.exec(t, qRename, holder.ID, "Dana Whitfield")
	id := e.openRoomAt(t, StatusNew, e.now)

	require.NoError(t, e.alerts.Assign(e.ctx, id, departed.ID, dispatcher.ID))
	e.exec(t, qDropUser, departed.ID)
	require.NoError(t, e.alerts.Assign(e.ctx, id, holder.ID, dispatcher.ID))
	_, err := e.alerts.Comment(e.ctx, id, holder.ID, "swapping the failed disk")
	require.NoError(t, err)
	require.NoError(t, e.alerts.Assign(e.ctx, id, stranger.ID, dispatcher.ID))

	want := map[uuid.UUID]string{dispatcher.ID: "Sam Okafor", holder.ID: "Dana Whitfield"}
	assert.Equal(t, want, e.opened(t, id).People)

	// The administrator flag widens the row policy to every tenant; the read still names its own.
	admin := dbtx.WithDefaultTenant(context.Background(), true)
	room, err := e.alerts.Investigation(admin, id, uuid.Nil)
	require.NoError(t, err)
	assert.Equal(t, want, room.People)
}

func TestARoomNobodyTouchedNamesNobody(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	id := e.openRoomAt(t, StatusNew, e.now)

	assert.Empty(t, e.opened(t, id).People)
}
