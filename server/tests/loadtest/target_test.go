package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStagingTargetsAreAccepted(t *testing.T) {
	for _, target := range []string{
		"http://opengate-staging-server:8080",
		"http://opengate-staging-server.opengate-staging.svc.cluster.local:8080",
		"http://localhost:8080",
		"http://127.0.0.1:8080",
		"http://server:8080",
	} {
		t.Run(target, func(t *testing.T) {
			assert.NoError(t, CheckTarget(target))
		})
	}
}

func TestProductionTargetsAreRefused(t *testing.T) {
	for _, target := range []string{
		"http://opengate-server.opengate.svc.cluster.local:8080",
		"https://opengate.example.com",
		"http://opengate-server:8080",
		"http://opengate-postgres.opengate:5432",
	} {
		t.Run(target, func(t *testing.T) {
			err := CheckTarget(target)
			require.Error(t, err, "a production target must be refused")
			assert.Contains(t, err.Error(), "not an allowed load-test target")
		})
	}
}

func TestAnUnknownHostIsRefusedRatherThanAdmitted(t *testing.T) {
	err := CheckTarget("http://somewhere-nobody-listed:8080")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not an allowed load-test target")
}

func TestAMalformedTargetIsRefused(t *testing.T) {
	for _, target := range []string{"", "://nope", "not a url at all", "http://"} {
		t.Run(target, func(t *testing.T) {
			assert.Error(t, CheckTarget(target))
		})
	}
}

func TestQUICAddressesUseTheSameAllowlist(t *testing.T) {
	assert.NoError(t, CheckQUICAddress("opengate-staging-server:9090"))
	assert.NoError(t, CheckQUICAddress("10.0.0.42:9090"))
	assert.NoError(t, CheckQUICAddress("127.0.0.1:9090"))

	err := CheckQUICAddress("opengate-server.opengate:9090")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not an allowed load-test target")
}

func TestPrivateAddressesAreAllowedAndPublicOnesAreNot(t *testing.T) {
	assert.NoError(t, CheckQUICAddress("10.244.1.7:9090"))
	assert.NoError(t, CheckQUICAddress("192.168.1.5:9090"))
	assert.NoError(t, CheckQUICAddress("172.16.0.3:9090"))

	assert.Error(t, CheckQUICAddress("8.8.8.8:9090"))
	assert.Error(t, CheckQUICAddress("140.238.1.1:9090"))
}

func TestTheProductionNamespaceIsRefusedInEveryAddressForm(t *testing.T) {
	for _, target := range []string{
		"http://opengate-server.opengate:8080",
		"http://opengate-server.opengate.svc:8080",
		"http://opengate-server.opengate.svc.cluster.local:8080",
	} {
		t.Run(target, func(t *testing.T) {
			assert.Error(t, CheckTarget(target))
		})
	}
}
