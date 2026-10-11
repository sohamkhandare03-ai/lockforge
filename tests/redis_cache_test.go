package tests

import (
	"context"
	"testing"
	"time"

	"github.com/lockforge/lockforge/internal/redisadapter"
)

func TestRedisAdapterMemoryFallback(t *testing.T) {
	// Empty addr forces in-memory fallback
	adapter := redisadapter.NewAdapter("")
	ctx := context.Background()

	resourceID := "cached-resource-1"

	// 1. Initially cache should miss
	cached, ok := adapter.CacheGet(ctx, resourceID)
	if ok || cached != nil {
		t.Fatalf("Expected cache miss for empty cache")
	}

	// 2. Set cache entry
	lockData := &redisadapter.CachedLock{
		ResourceID:   resourceID,
		OwnerID:      "worker-redis",
		FencingToken: 105,
		ExpiresAtMs:  time.Now().Add(10 * time.Second).UnixMilli(),
	}
	err := adapter.CacheSet(ctx, resourceID, lockData, 10*time.Second)
	if err != nil {
		t.Fatalf("Failed to set cache entry: %v", err)
	}

	// 3. Cache get should now hit
	cached, ok = adapter.CacheGet(ctx, resourceID)
	if !ok || cached == nil {
		t.Fatalf("Expected cache hit after set")
	}
	if cached.OwnerID != "worker-redis" || cached.FencingToken != 105 {
		t.Fatalf("Cache data mismatch: %+v", cached)
	}

	// 4. Cache delete
	err = adapter.CacheDelete(ctx, resourceID)
	if err != nil {
		t.Fatalf("Failed to delete cache entry: %v", err)
	}

	// 5. Subsequent get should miss
	cached, ok = adapter.CacheGet(ctx, resourceID)
	if ok || cached != nil {
		t.Fatalf("Expected cache miss after deletion")
	}
}

func TestRedisAdapterTTLAutoExpiry(t *testing.T) {
	adapter := redisadapter.NewAdapter("")
	ctx := context.Background()

	resourceID := "short-lived-cache-resource"

	// Store with 200ms expiration
	lockData := &redisadapter.CachedLock{
		ResourceID:   resourceID,
		OwnerID:      "ephemeral-worker",
		FencingToken: 999,
		ExpiresAtMs:  time.Now().Add(200 * time.Millisecond).UnixMilli(),
	}
	_ = adapter.CacheSet(ctx, resourceID, lockData, 200*time.Millisecond)

	// Immediate get hits
	cached, ok := adapter.CacheGet(ctx, resourceID)
	if !ok || cached == nil {
		t.Fatal("Expected immediate cache hit")
	}

	// Sleep past expiration
	time.Sleep(300 * time.Millisecond)

	// Get after expiry must miss
	cachedAfter, okAfter := adapter.CacheGet(ctx, resourceID)
	if okAfter || cachedAfter != nil {
		t.Fatal("Expected cache miss after TTL expired")
	}
}
