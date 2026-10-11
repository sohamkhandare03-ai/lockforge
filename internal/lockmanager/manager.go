package lockmanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/lockforge/lockforge/internal/etcdclient"
	"github.com/lockforge/lockforge/internal/events"
	"github.com/lockforge/lockforge/internal/fencing"
	"github.com/lockforge/lockforge/internal/redisadapter"
	pb "github.com/lockforge/lockforge/proto"
)

const (
	LockKeyPrefix    = "/lockforge/locks/"
	DefaultTTLSecond = 10
)

var (
	ErrLockAlreadyHeld = errors.New("lock already held by another client")
	ErrLockNotFound    = errors.New("lock not found")
	ErrNotLockOwner    = errors.New("client is not the current lock owner")
)

// LockPayload stores serialized JSON metadata in etcd.
type LockPayload struct {
	ResourceID        string `json:"resource_id"`
	OwnerID           string `json:"owner_id"`
	LeaseID           int64  `json:"lease_id"`
	FencingToken      int64  `json:"fencing_token"`
	AcquiredAtUnixMs  int64  `json:"acquired_at_unix_ms"`
	ExpiresAtUnixMs   int64  `json:"expires_at_unix_ms"`
	TTLSeconds        int64  `json:"ttl_seconds"`
}

// Config holds settings for LockManager.
type Config struct {
	DefaultTTL int64
}

// LockManager coordinates distributed locking with Raft consensus, leases, and fencing.
type LockManager struct {
	etcd       etcdclient.Client
	fencing    *fencing.Generator
	redis      *redisadapter.Adapter
	events     *events.RingBuffer
	cfg        Config
	cancelFunc context.CancelFunc
	wg         sync.WaitGroup
}

// NewLockManager creates and starts a LockManager instance.
func NewLockManager(
	client etcdclient.Client,
	fencingGen *fencing.Generator,
	redisAdapt *redisadapter.Adapter,
	eventBuffer *events.RingBuffer,
	cfg Config,
) *LockManager {
	if cfg.DefaultTTL <= 0 {
		cfg.DefaultTTL = DefaultTTLSecond
	}

	ctx, cancel := context.WithCancel(context.Background())
	lm := &LockManager{
		etcd:       client,
		fencing:    fencingGen,
		redis:      redisAdapt,
		events:     eventBuffer,
		cfg:        cfg,
		cancelFunc: cancel,
	}

	lm.wg.Add(1)
	go lm.watchLockEvents(ctx)

	return lm
}

// Close gracefully stops background watchers.
func (lm *LockManager) Close() {
	if lm.cancelFunc != nil {
		lm.cancelFunc()
	}
	lm.wg.Wait()
}

// GetEventBuffer exposes the event ring buffer.
func (lm *LockManager) GetEventBuffer() *events.RingBuffer {
	return lm.events
}

// GetRedisAdapter exposes the Redis cache adapter.
func (lm *LockManager) GetRedisAdapter() *redisadapter.Adapter {
	return lm.redis
}

// GetEtcdClient exposes the etcd client.
func (lm *LockManager) GetEtcdClient() etcdclient.Client {
	return lm.etcd
}

// watchLockEvents listens to etcd watch events to react to lease expirations and external releases.
func (lm *LockManager) watchLockEvents(ctx context.Context) {
	defer lm.wg.Done()
	watchCh := lm.etcd.Watch(ctx, LockKeyPrefix)

	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-watchCh:
			if !ok {
				return
			}
			if ev.Type == etcdclient.WatchEventDelete {
				var key string
				var prevVal []byte
				if ev.Prev != nil {
					key = ev.Prev.Key
					prevVal = ev.Prev.Value
				} else if ev.Kv != nil {
					key = ev.Kv.Key
				}

				resID := strings.TrimPrefix(key, LockKeyPrefix)
				_ = lm.redis.CacheDelete(ctx, resID)

				var prevPayload LockPayload
				if len(prevVal) > 0 {
					_ = json.Unmarshal(prevVal, &prevPayload)
				}

				// If it expired, prevPayload might contain the lease id
				lm.events.Publish(
					pb.LockEventType_LOCK_EVENT_TYPE_EXPIRED,
					resID,
					prevPayload.OwnerID,
					prevPayload.FencingToken,
					prevPayload.LeaseID,
					fmt.Sprintf("Lease expired or key deleted for resource '%s'", resID),
				)
			}
		}
	}
}

// AcquireLock atomically acquires a lock using etcd Compare-And-Swap transaction.
func (lm *LockManager) AcquireLock(ctx context.Context, resourceID, clientID string, ttlSeconds int64) (*pb.AcquireLockResponse, error) {
	if resourceID == "" {
		return nil, errors.New("resource_id is required")
	}
	if clientID == "" {
		return nil, errors.New("client_id is required")
	}
	if ttlSeconds <= 0 {
		ttlSeconds = lm.cfg.DefaultTTL
	}

	lockKey := LockKeyPrefix + resourceID

	// 1. Grant etcd lease
	leaseID, err := lm.etcd.GrantLease(ctx, ttlSeconds)
	if err != nil {
		return &pb.AcquireLockResponse{
			Acquired:     false,
			ResourceId:   resourceID,
			ClientId:     clientID,
			ErrorMessage: fmt.Sprintf("failed to grant lease: %v", err),
		}, nil
	}

	// 2. Issue monotonic fencing token
	fencingToken, err := lm.fencing.NextToken(ctx, resourceID)
	if err != nil {
		_ = lm.etcd.RevokeLease(ctx, leaseID)
		return nil, fmt.Errorf("failed to issue fencing token: %w", err)
	}

	now := time.Now()
	expiresAt := now.Add(time.Duration(ttlSeconds) * time.Second)

	payload := LockPayload{
		ResourceID:       resourceID,
		OwnerID:          clientID,
		LeaseID:          int64(leaseID),
		FencingToken:     fencingToken,
		AcquiredAtUnixMs: now.UnixMilli(),
		ExpiresAtUnixMs:  expiresAt.UnixMilli(),
		TTLSeconds:       ttlSeconds,
	}

	valBytes, err := json.Marshal(payload)
	if err != nil {
		_ = lm.etcd.RevokeLease(ctx, leaseID)
		return nil, err
	}

	// 3. CompareAndSwap Txn: Key must not exist (CreateRevision == 0)
	txnResp, err := lm.etcd.Txn(ctx).
		If(etcdclient.CompareCreateRevisionEqual(lockKey, 0)).
		Then(etcdclient.OpPut(lockKey, string(valBytes), leaseID)).
		Commit(ctx)

	if err != nil {
		_ = lm.etcd.RevokeLease(ctx, leaseID)
		return &pb.AcquireLockResponse{
			Acquired:     false,
			ResourceId:   resourceID,
			ClientId:     clientID,
			ErrorMessage: fmt.Sprintf("etcd transaction failed: %v", err),
		}, nil
	}

	if !txnResp.Succeeded {
		// Key already held! Revoke unused lease
		_ = lm.etcd.RevokeLease(ctx, leaseID)

		// Inspect current holder to inform caller
		currOwner := ""
		if getResp, gErr := lm.etcd.Get(ctx, lockKey); gErr == nil && len(getResp.Kvs) > 0 {
			var curr LockPayload
			if json.Unmarshal(getResp.Kvs[0].Value, &curr) == nil {
				currOwner = curr.OwnerID
			}
		}

		return &pb.AcquireLockResponse{
			Acquired:       false,
			ResourceId:     resourceID,
			ClientId:       clientID,
			CurrentOwnerId: currOwner,
			ErrorMessage:   fmt.Sprintf("lock already held by client '%s'", currOwner),
		}, nil
	}

	// 4. Update Redis read cache
	_ = lm.redis.CacheSet(ctx, resourceID, &redisadapter.CachedLock{
		ResourceID:   resourceID,
		OwnerID:      clientID,
		FencingToken: fencingToken,
		ExpiresAtMs:  expiresAt.UnixMilli(),
	}, time.Duration(ttlSeconds)*time.Second)

	// 5. Publish event
	lm.events.Publish(
		pb.LockEventType_LOCK_EVENT_TYPE_ACQUIRED,
		resourceID,
		clientID,
		fencingToken,
		int64(leaseID),
		fmt.Sprintf("Lock acquired on '%s' by '%s' with fencing token %d (TTL %ds)", resourceID, clientID, fencingToken, ttlSeconds),
	)

	return &pb.AcquireLockResponse{
		Acquired:          true,
		ResourceId:        resourceID,
		ClientId:          clientID,
		LeaseId:           int64(leaseID),
		FencingToken:      fencingToken,
		AcquiredAtUnixMs:  now.UnixMilli(),
		ExpiresAtUnixMs:   expiresAt.UnixMilli(),
		CurrentOwnerId:    clientID,
	}, nil
}

// ReleaseLock verifies ownership and atomically deletes the key & revokes lease.
func (lm *LockManager) ReleaseLock(ctx context.Context, resourceID, clientID string, leaseID, fencingToken int64) (*pb.ReleaseLockResponse, error) {
	if resourceID == "" {
		return nil, errors.New("resource_id is required")
	}

	lockKey := LockKeyPrefix + resourceID
	getResp, err := lm.etcd.Get(ctx, lockKey)
	if err != nil {
		return nil, fmt.Errorf("failed to query lock: %w", err)
	}

	if len(getResp.Kvs) == 0 {
		return &pb.ReleaseLockResponse{
			Released:   false,
			ResourceId: resourceID,
			Message:    "lock does not exist or already expired",
		}, nil
	}

	var payload LockPayload
	if err := json.Unmarshal(getResp.Kvs[0].Value, &payload); err != nil {
		return nil, fmt.Errorf("invalid lock payload: %w", err)
	}

	if clientID != "" && payload.OwnerID != clientID {
		return &pb.ReleaseLockResponse{
			Released:   false,
			ResourceId: resourceID,
			Message:    fmt.Sprintf("permission denied: lock held by '%s', not '%s'", payload.OwnerID, clientID),
		}, nil
	}

	// Delete key
	_, err = lm.etcd.Delete(ctx, lockKey)
	if err != nil {
		return nil, fmt.Errorf("failed to delete lock key: %w", err)
	}

	// Revoke lease if active
	if payload.LeaseID > 0 {
		_ = lm.etcd.RevokeLease(ctx, etcdclient.LeaseID(payload.LeaseID))
	}

	// Invalidate cache
	_ = lm.redis.CacheDelete(ctx, resourceID)

	// Publish event
	lm.events.Publish(
		pb.LockEventType_LOCK_EVENT_TYPE_RELEASED,
		resourceID,
		payload.OwnerID,
		payload.FencingToken,
		payload.LeaseID,
		fmt.Sprintf("Lock released on '%s' by '%s'", resourceID, payload.OwnerID),
	)

	return &pb.ReleaseLockResponse{
		Released:   true,
		ResourceId: resourceID,
		Message:    "lock successfully released",
	}, nil
}

// RenewLease extends lease TTL.
func (lm *LockManager) RenewLease(ctx context.Context, resourceID, clientID string, leaseID, ttlSeconds int64) (*pb.RenewLeaseResponse, error) {
	if resourceID == "" {
		return nil, errors.New("resource_id is required")
	}

	lockKey := LockKeyPrefix + resourceID
	getResp, err := lm.etcd.Get(ctx, lockKey)
	if err != nil || len(getResp.Kvs) == 0 {
		return &pb.RenewLeaseResponse{
			Renewed:    false,
			ResourceId: resourceID,
			Message:    "lock not found or expired",
		}, nil
	}

	var payload LockPayload
	if err := json.Unmarshal(getResp.Kvs[0].Value, &payload); err != nil {
		return nil, err
	}

	if clientID != "" && payload.OwnerID != clientID {
		return &pb.RenewLeaseResponse{
			Renewed:    false,
			ResourceId: resourceID,
			Message:    "permission denied: client is not lock owner",
		}, nil
	}

	effectiveLeaseID := leaseID
	if effectiveLeaseID <= 0 {
		effectiveLeaseID = payload.LeaseID
	}
	if ttlSeconds <= 0 {
		ttlSeconds = payload.TTLSeconds
	}

	_, err = lm.etcd.KeepAliveOnce(ctx, etcdclient.LeaseID(effectiveLeaseID), ttlSeconds)
	if err != nil {
		return &pb.RenewLeaseResponse{
			Renewed:    false,
			ResourceId: resourceID,
			Message:    fmt.Sprintf("failed to keep alive lease: %v", err),
		}, nil
	}

	newExpiresAt := time.Now().Add(time.Duration(ttlSeconds) * time.Second)
	payload.ExpiresAtUnixMs = newExpiresAt.UnixMilli()
	payload.TTLSeconds = ttlSeconds
	newVal, _ := json.Marshal(payload)
	_, _ = lm.etcd.Put(ctx, lockKey, string(newVal), etcdclient.LeaseID(effectiveLeaseID))

	_ = lm.redis.CacheSet(ctx, resourceID, &redisadapter.CachedLock{
		ResourceID:   resourceID,
		OwnerID:      payload.OwnerID,
		FencingToken: payload.FencingToken,
		ExpiresAtMs:  newExpiresAt.UnixMilli(),
	}, time.Duration(ttlSeconds)*time.Second)

	lm.events.Publish(
		pb.LockEventType_LOCK_EVENT_TYPE_RENEWED,
		resourceID,
		payload.OwnerID,
		payload.FencingToken,
		effectiveLeaseID,
		fmt.Sprintf("Lease renewed for '%s' (+%ds)", resourceID, ttlSeconds),
	)

	return &pb.RenewLeaseResponse{
		Renewed:              true,
		ResourceId:           resourceID,
		NewExpiresAtUnixMs:   newExpiresAt.UnixMilli(),
		Message:              "lease successfully renewed",
	}, nil
}

// InspectLock returns current lock state using Redis read-shortcut when possible.
func (lm *LockManager) InspectLock(ctx context.Context, resourceID string) (*pb.InspectLockResponse, error) {
	if resourceID == "" {
		return nil, errors.New("resource_id is required")
	}

	// 1. Query etcd for authoritative state
	lockKey := LockKeyPrefix + resourceID
	getResp, err := lm.etcd.Get(ctx, lockKey)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect lock: %w", err)
	}

	if len(getResp.Kvs) == 0 {
		return &pb.InspectLockResponse{
			ResourceId: resourceID,
			IsLocked:   false,
		}, nil
	}

	var payload LockPayload
	if err := json.Unmarshal(getResp.Kvs[0].Value, &payload); err != nil {
		return nil, err
	}

	ttlRemaining, _ := lm.etcd.TimeToLive(ctx, etcdclient.LeaseID(payload.LeaseID))
	if ttlRemaining <= 0 {
		// Calculate from expiresAt
		remaining := time.Until(time.UnixMilli(payload.ExpiresAtUnixMs)).Seconds()
		if remaining > 0 {
			ttlRemaining = int64(remaining)
		} else {
			ttlRemaining = 0
		}
	}

	return &pb.InspectLockResponse{
		ResourceId:          resourceID,
		IsLocked:            true,
		OwnerId:             payload.OwnerID,
		LeaseId:             payload.LeaseID,
		FencingToken:        payload.FencingToken,
		AcquiredAtUnixMs:    payload.AcquiredAtUnixMs,
		ExpiresAtUnixMs:     payload.ExpiresAtUnixMs,
		TtlRemainingSeconds: ttlRemaining,
	}, nil
}

// GetActiveLocks returns all currently held locks across the cluster.
func (lm *LockManager) GetActiveLocks(ctx context.Context) ([]*pb.LockMetadata, error) {
	resp, err := lm.etcd.GetPrefix(ctx, LockKeyPrefix)
	if err != nil {
		return nil, fmt.Errorf("failed to list active locks: %w", err)
	}

	var list []*pb.LockMetadata
	now := time.Now().UnixMilli()

	for _, kv := range resp.Kvs {
		var payload LockPayload
		if err := json.Unmarshal(kv.Value, &payload); err != nil {
			continue
		}

		if payload.ExpiresAtUnixMs > 0 && now > payload.ExpiresAtUnixMs {
			continue // Expired
		}

		list = append(list, &pb.LockMetadata{
			ResourceId:       payload.ResourceID,
			OwnerId:          payload.OwnerID,
			LeaseId:          payload.LeaseID,
			FencingToken:     payload.FencingToken,
			AcquiredAtUnixMs: payload.AcquiredAtUnixMs,
			ExpiresAtUnixMs:  payload.ExpiresAtUnixMs,
			TtlSeconds:       payload.TTLSeconds,
		})
	}

	return list, nil
}
