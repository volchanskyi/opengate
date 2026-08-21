package acceptance

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// TestAnAdministratorPublishesABuildAndTheMachineAcknowledgesIt is the
// sentence Agent Updates promises: a build is published, signed by this
// installation, pushed to the machines that can take it, and the machine's
// answer is what the update page shows.
func TestAnAdministratorPublishesABuildAndTheMachineAcknowledgesIt(t *testing.T) {
	t.Parallel()

	product := newProduct(t)
	contoso := product.arrangeCustomer("Contoso")
	admin := product.Administrator(contoso)

	machine := product.Machine(admin.mintEnrolmentToken("Head Office").Token, "contoso-desk-01")
	machine.AwaitOnline()

	const version = "1.2.3"
	manifest := admin.publishBuild(version)
	assert.NotEmpty(t, manifest.Signature,
		"a build nobody signed is a build a machine must not install")

	var pushed struct {
		PushedCount int `json:"pushed_count"`
	}
	reply := admin.Post("/api/v1/updates/push", map[string]any{
		"version": version, "os": "linux", "arch": "amd64",
	})
	require.Equalf(t, http.StatusOK, reply.Status, "pushing the build failed: %s", reply.Text())
	reply.Into(&pushed)
	assert.Equal(t, 1, pushed.PushedCount, "the machine that can take it is the machine that gets it")

	offered := machine.Await(protocol.MsgAgentUpdate)
	assert.Equal(t, version, offered.Version, "the machine is offered the build that was published")

	applied := true
	machine.Send(&protocol.ControlMessage{
		Type: protocol.MsgAgentUpdateAck, Version: version, Success: &applied,
	})

	require.Eventually(t, func() bool {
		var rollout []struct {
			DeviceID string `json:"device_id"`
			Status   string `json:"status"`
		}
		admin.Get("/api/v1/updates/status/" + version).Into(&rollout)
		for _, row := range rollout {
			if row.DeviceID == machine.DeviceID.String() && row.Status == "success" {
				return true
			}
		}
		return false
	}, eventually, poll, "what the machine answered is what the update page shows")
}

// TestABuildIsNotPushedToAMachineOfAnotherShape pins the bound on a push: a
// build for one operating system and processor must not be offered to a
// machine that cannot run it.
func TestABuildIsNotPushedToAMachineOfAnotherShape(t *testing.T) {
	t.Parallel()

	product := newProduct(t)
	contoso := product.arrangeCustomer("Contoso")
	admin := product.Administrator(contoso)

	machine := product.Machine(admin.mintEnrolmentToken("Head Office").Token, "contoso-desk-02")
	machine.AwaitOnline()

	const version = "9.9.9"
	admin.publishBuild(version, "windows", "arm64")

	var pushed struct {
		PushedCount int `json:"pushed_count"`
	}
	admin.Post("/api/v1/updates/push", map[string]any{
		"version": version, "os": "windows", "arch": "arm64",
	}).Into(&pushed)

	assert.Zero(t, pushed.PushedCount)
	assert.False(t, machine.Received(protocol.MsgAgentUpdate),
		"a Linux machine is never offered a Windows build")
}

// agentManifest is a published build as the update page lists it.
type agentManifest struct {
	Version   string `json:"version"`
	Signature string `json:"signature"`
}

// publishBuild is an administrator publishing a signed build. The shape
// defaults to the one the machines in these tests report.
func (a *Technician) publishBuild(version string, shape ...string) agentManifest {
	a.t.Helper()

	osName, arch := "linux", "amd64"
	if len(shape) == 2 {
		osName, arch = shape[0], shape[1]
	}

	digest := sha256.Sum256([]byte("agent-" + version))
	reply := a.Post("/api/v1/updates/manifests", map[string]any{
		"version": version, "os": osName, "arch": arch,
		"url": "https://downloads.example.com/agent-" + version, "sha256": hex.EncodeToString(digest[:]),
	})
	require.Equalf(a.t, http.StatusOK, reply.Status, "publishing the build failed: %s", reply.Text())

	var manifest agentManifest
	reply.Into(&manifest)
	require.Equal(a.t, version, manifest.Version)
	return manifest
}
