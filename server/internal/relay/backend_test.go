package relay_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/relay"
)

func TestSessionRegistryFromConfig_Backends(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		backend  string
		cfg      relay.RedisConfig
		wantType any
		wantErr  bool
	}{
		{"empty defaults to in-process", "", relay.RedisConfig{}, &relay.InProcessRegistry{}, false},
		{"explicit inprocess", "inprocess", relay.RedisConfig{}, &relay.InProcessRegistry{}, false},
		{"redis with addr builds RedisRegistry", "redis", relay.RedisConfig{Addr: "127.0.0.1:6379"}, &relay.RedisRegistry{}, false},
		{"redis without connection config rejected", "redis", relay.RedisConfig{}, nil, true},
		{"unknown backend rejected", "bogus", relay.RedisConfig{}, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reg, closer, err := relay.SessionRegistryFromConfig(tc.backend, tc.cfg)
			if tc.wantErr {
				require.ErrorIs(t, err, relay.ErrInvalidArgument)
				return
			}
			require.NoError(t, err)
			require.IsType(t, tc.wantType, reg)
			require.NoError(t, closer.Close())
		})
	}
}

func TestRedisUniversalOptions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name           string
		cfg            relay.RedisConfig
		wantAddrs      []string
		wantMasterName string
		wantPassword   string
		wantErr        bool
	}{
		{"single instance from addr", relay.RedisConfig{Addr: "redis:6379"}, []string{"redis:6379"}, "", "", false},
		{"sentinel from master and addrs", relay.RedisConfig{SentinelAddrs: []string{"s1:26379", "s2:26379"}, MasterName: "opengate"}, []string{"s1:26379", "s2:26379"}, "opengate", "", false},
		{"password is carried through", relay.RedisConfig{Addr: "redis:6379", Password: "s3cr3t"}, []string{"redis:6379"}, "", "s3cr3t", false},
		{"sentinel addrs without master rejected", relay.RedisConfig{SentinelAddrs: []string{"s1:26379"}}, nil, "", "", true},
		{"master without sentinel addrs rejected", relay.RedisConfig{MasterName: "opengate"}, nil, "", "", true},
		{"empty config rejected", relay.RedisConfig{}, nil, "", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opts, err := relay.RedisUniversalOptions(tc.cfg)
			if tc.wantErr {
				require.ErrorIs(t, err, relay.ErrInvalidArgument)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantAddrs, opts.Addrs)
			require.Equal(t, tc.wantMasterName, opts.MasterName)
			require.Equal(t, tc.wantPassword, opts.Password)
		})
	}
}
