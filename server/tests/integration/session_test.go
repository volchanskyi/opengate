package integration

import (
	"context"
	"github.com/volchanskyi/opengate/server/internal/agentapi"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/cert"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"github.com/volchanskyi/opengate/server/internal/signaling"
	"github.com/volchanskyi/opengate/server/internal/updater"
	"net/http/httptest"
)

const (
	pathSessions = "/api/v1/sessions"
	bearerPrefix = "Bearer "
)

// sessionTestEnv bundles all dependencies for session integration tests.
type sessionTestEnv struct {
	store         *db.PostgresStore
	devices       device.Repository
	deviceUpdates updater.DeviceUpdateRepository
	certMgr       *cert.Manager
	relay         *relay.Relay
	agentSrv      *agentapi.AgentServer
	agentAddr     string
	httpSrv       *httptest.Server
	jwt           *auth.JWTConfig
	sigTracker    *signaling.Tracker
	signing       *updater.SigningKeys
	manifests     *updater.ManifestStore
	cancel        context.CancelFunc
}
