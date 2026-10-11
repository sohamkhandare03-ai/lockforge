package redisadapter

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// CachedLock stores cached lock status in Redis.
type CachedLock struct {
	ResourceID   string `json:"resource_id"`
	OwnerID      string `json:"owner_id"`
	FencingToken int64  `json:"fencing_token"`
	ExpiresAtMs  int64  `json:"expires_at_ms"`
}

// Adapter provides read-shortcut caching for lock state.
// Invariant: etcd is the single source of truth; Redis is only a cache.
type Adapter struct {
	rdb        *redis.Client
	mu         sync.RWMutex
	memory     map[string]string
	isReal     bool
	statsHit   int64
	statsMiss  int64
}

// NewAdapter creates a Redis cache adapter.
func NewAdapter(addr string) *Adapter {
	if addr == "" {
		return &Adapter{
			memory: make(map[string]string),
			isReal: false,
		}
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:         addr,
		DialTimeout:  1 * time.Second,
		ReadTimeout:  500 * time.Millisecond,
		WriteTimeout: 500 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		// Redis unavailable, fallback to memory
		return &Adapter{
			memory: make(map[string]string),
			isReal: false,
		}
	}

	return &Adapter{
		rdb:    rdb,
		memory: make(map[string]string),
		isReal: true,
	}
}

// CacheSet saves lock state with expiration.
func (a *Adapter) CacheSet(ctx context.Context, resourceID string, lock *CachedLock, ttl time.Duration) error {
	data, err := json.Marshal(lock)
	if err != nil {
		return err
	}

	key := fmt.Sprintf("lockforge:cache:%s", resourceID)
	if a.isReal && a.rdb != nil {
		return a.rdb.Set(ctx, key, data, ttl).Err()
	}

	a.mu.Lock()
	a.memory[key] = string(data)
	a.mu.Unlock()
	return nil
}

// CacheGet retrieves cached lock state if available.
func (a *Adapter) CacheGet(ctx context.Context, resourceID string) (*CachedLock, bool) {
	key := fmt.Sprintf("lockforge:cache:%s", resourceID)
	var raw string

	if a.isReal && a.rdb != nil {
		val, err := a.rdb.Get(ctx, key).Result()
		if err != nil {
			a.mu.Lock()
			a.statsMiss++
			a.mu.Unlock()
			return nil, false
		}
		raw = val
	} else {
		a.mu.RLock()
		val, ok := a.memory[key]
		a.mu.RUnlock()
		if !ok {
			a.mu.Lock()
			a.statsMiss++
			a.mu.Unlock()
			return nil, false
		}
		raw = val
	}

	var lock CachedLock
	if err := json.Unmarshal([]byte(raw), &lock); err != nil {
		return nil, false
	}

	if lock.ExpiresAtMs > 0 && time.Now().UnixMilli() > lock.ExpiresAtMs {
		_ = a.CacheDelete(ctx, resourceID)
		return nil, false
	}

	a.mu.Lock()
	a.statsHit++
	a.mu.Unlock()
	return &lock, true
}

// CacheDelete removes a cached lock entry upon release or expiry.
func (a *Adapter) CacheDelete(ctx context.Context, resourceID string) error {
	key := fmt.Sprintf("lockforge:cache:%s", resourceID)
	if a.isReal && a.rdb != nil {
		return a.rdb.Del(ctx, key).Err()
	}

	a.mu.Lock()
	delete(a.memory, key)
	a.mu.Unlock()
	return nil
}

// IsHealthy returns true if the Redis connection is active.
func (a *Adapter) IsHealthy(ctx context.Context) bool {
	if !a.isReal || a.rdb == nil {
		return true // In-memory mock is always healthy
	}
	return a.rdb.Ping(ctx).Err() == nil
}
