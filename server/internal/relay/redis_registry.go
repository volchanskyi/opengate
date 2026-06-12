package relay

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

const (
	redisKeyPrefix     = "opengate:relay:"
	redisEventsChannel = redisKeyPrefix + "events"
	// redisMetaTTL bounds how long session metadata lingers if DeleteSession
	// never runs (e.g. owner crash) — a backstop against leaks. It is generous
	// relative to the affinity TTL; an active owner re-saves on lifecycle
	// changes, refreshing it.
	redisMetaTTL = 24 * time.Hour
)

// claimAffinityScript atomically claims ownership: it returns the existing
// owner if the key is already set, otherwise sets the key (with TTL) to the
// caller's serverID and returns it. Doing this in one round-trip removes the
// SETNX-then-GET race a two-call sequence would have.
//
//	KEYS[1] = affinity key, ARGV[1] = serverID, ARGV[2] = ttl seconds
var claimAffinityScript = redis.NewScript(`
local cur = redis.call('GET', KEYS[1])
if cur then return cur end
redis.call('SET', KEYS[1], ARGV[1], 'EX', ARGV[2])
return ARGV[1]
`)

// RedisRegistry is the SessionRegistry adapter backed by Redis, for
// multi-server relay pools (ADR-023). Affinity ownership is an
// atomic claim-or-get with TTL; session metadata is a JSON value; lifecycle
// events ride Redis Pub/Sub. It is the cross-server-capable replacement for
// InProcessRegistry, selected via REGISTRY_BACKEND=redis (cmd/meshserver).
type RedisRegistry struct {
	client redis.UniversalClient
}

// NewRedisRegistry returns a SessionRegistry backed by the given Redis client
// (a plain client or a Sentinel failover client — both satisfy
// redis.UniversalClient).
func NewRedisRegistry(client redis.UniversalClient) *RedisRegistry {
	return &RedisRegistry{client: client}
}

func affinityKey(token protocol.SessionToken) string {
	return redisKeyPrefix + "affinity:" + string(token)
}

func metaKey(token protocol.SessionToken) string {
	return redisKeyPrefix + "meta:" + string(token)
}

// ClaimAffinity implements SessionRegistry.
func (r *RedisRegistry) ClaimAffinity(ctx context.Context, token protocol.SessionToken, serverID string, ttl time.Duration) (string, error) {
	if serverID == "" {
		return "", ErrInvalidArgument
	}
	ttlSecs := int(ttl.Seconds())
	if ttlSecs < 1 {
		ttlSecs = 1
	}
	owner, err := claimAffinityScript.Run(ctx, r.client, []string{affinityKey(token)}, serverID, ttlSecs).Text()
	if err != nil {
		return "", err
	}
	return owner, nil
}

// LookupOwner implements SessionRegistry.
func (r *RedisRegistry) LookupOwner(ctx context.Context, token protocol.SessionToken) (string, error) {
	owner, err := r.client.Get(ctx, affinityKey(token)).Result()
	if errors.Is(err, redis.Nil) {
		return "", ErrRegistryNotFound
	}
	if err != nil {
		return "", err
	}
	return owner, nil
}

// SaveSession implements SessionRegistry. Persists metadata and ensures an
// affinity entry owned by meta.ServerID exists without overwriting an existing
// claim (matching InProcessRegistry).
func (r *RedisRegistry) SaveSession(ctx context.Context, token protocol.SessionToken, meta SessionMeta) error {
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	if err := r.client.SetNX(ctx, affinityKey(token), meta.ServerID, redisMetaTTL).Err(); err != nil {
		return err
	}
	return r.client.Set(ctx, metaKey(token), data, redisMetaTTL).Err()
}

// DeleteSession implements SessionRegistry. Releases the affinity claim and
// removes metadata; a no-op when the token has no entry.
func (r *RedisRegistry) DeleteSession(ctx context.Context, token protocol.SessionToken) error {
	return r.client.Del(ctx, affinityKey(token), metaKey(token)).Err()
}

// SubscribeEvents implements SessionRegistry. The returned channel closes when
// ctx is cancelled. The Redis subscription is confirmed before returning so a
// subsequent PublishEvent is delivered (Redis Pub/Sub drops messages that have
// no subscriber).
func (r *RedisRegistry) SubscribeEvents(ctx context.Context) (<-chan SessionEvent, error) {
	pubsub := r.client.Subscribe(ctx, redisEventsChannel)
	if _, err := pubsub.Receive(ctx); err != nil {
		_ = pubsub.Close()
		return nil, err
	}

	out := make(chan SessionEvent, 16)
	go func() {
		defer close(out)
		defer func() { _ = pubsub.Close() }()
		ch := pubsub.Channel()
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				var evt SessionEvent
				if err := json.Unmarshal([]byte(msg.Payload), &evt); err != nil {
					continue
				}
				select {
				case out <- evt:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}

// PublishEvent implements SessionRegistry. Returns no error when there are no
// subscribers (Redis PUBLISH reports zero receivers).
func (r *RedisRegistry) PublishEvent(ctx context.Context, evt SessionEvent) error {
	data, err := json.Marshal(evt)
	if err != nil {
		return err
	}
	return r.client.Publish(ctx, redisEventsChannel, data).Err()
}

// Ping reports whether Redis is reachable. The readiness probe drains the pod
// when this fails (ADR-023 recovery posture).
func (r *RedisRegistry) Ping(ctx context.Context) error {
	return r.client.Ping(ctx).Err()
}
