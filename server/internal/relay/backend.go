package relay

import (
	"fmt"
	"io"

	"github.com/redis/go-redis/v9"
)

// RedisConfig holds the connection parameters for the Redis-backed registry.
// Exactly one connection mode is used: Sentinel (failover) when MasterName and
// SentinelAddrs are both set, otherwise a single instance via Addr.
type RedisConfig struct {
	Addr          string   // single-instance address (REDIS_ADDR), e.g. "redis:6379"
	SentinelAddrs []string // Sentinel addresses (REDIS_SENTINEL_ADDRS)
	MasterName    string   // Sentinel master group name (REDIS_MASTER_NAME)
	Password      string   // optional AUTH password (REDIS_PASSWORD); empty = no auth
}

// RedisUniversalOptions assembles go-redis UniversalOptions from cfg. Sentinel
// (failover) mode is selected when MasterName/SentinelAddrs are present — both
// are required — otherwise a single instance via Addr. Returns
// ErrInvalidArgument when the configuration is incomplete.
func RedisUniversalOptions(cfg RedisConfig) (*redis.UniversalOptions, error) {
	if cfg.MasterName != "" || len(cfg.SentinelAddrs) > 0 {
		if cfg.MasterName == "" {
			return nil, fmt.Errorf("%w: REDIS_MASTER_NAME is required with REDIS_SENTINEL_ADDRS", ErrInvalidArgument)
		}
		if len(cfg.SentinelAddrs) == 0 {
			return nil, fmt.Errorf("%w: REDIS_SENTINEL_ADDRS is required with REDIS_MASTER_NAME", ErrInvalidArgument)
		}
		// go-redis returns a FailoverClient when MasterName is set. The same
		// password authenticates the Sentinels and the data nodes.
		return &redis.UniversalOptions{
			Addrs:            cfg.SentinelAddrs,
			MasterName:       cfg.MasterName,
			Password:         cfg.Password,
			SentinelPassword: cfg.Password,
		}, nil
	}
	if cfg.Addr == "" {
		return nil, fmt.Errorf("%w: REDIS_ADDR (or REDIS_SENTINEL_ADDRS + REDIS_MASTER_NAME) is required for the redis backend", ErrInvalidArgument)
	}
	return &redis.UniversalOptions{Addrs: []string{cfg.Addr}, Password: cfg.Password}, nil
}

// noopCloser is the io.Closer returned for backends with no resources to
// release on shutdown (the in-process registry).
type noopCloser struct{}

func (noopCloser) Close() error { return nil }

// SessionRegistryFromConfig builds the SessionRegistry adapter selected by
// backend:
//
//   - "" or "inprocess" → InProcessRegistry (single-server default).
//   - "redis"           → RedisRegistry over a go-redis UniversalClient
//     assembled from redisCfg (single instance or Sentinel failover).
//
// The returned io.Closer releases adapter-held resources on shutdown — the
// Redis client for the redis backend, a no-op for in-process. An unknown
// backend or an incomplete Redis configuration returns ErrInvalidArgument.
func SessionRegistryFromConfig(backend string, redisCfg RedisConfig) (SessionRegistry, io.Closer, error) {
	switch backend {
	case "", "inprocess":
		return NewInProcessRegistry(), noopCloser{}, nil
	case "redis":
		opts, err := RedisUniversalOptions(redisCfg)
		if err != nil {
			return nil, nil, err
		}
		client := redis.NewUniversalClient(opts)
		return NewRedisRegistry(client), client, nil
	default:
		return nil, nil, fmt.Errorf("%w: unknown REGISTRY_BACKEND %q (want inprocess or redis)", ErrInvalidArgument, backend)
	}
}
