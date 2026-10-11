package tests

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/lockforge/lockforge/internal/etcdclient"
	"github.com/lockforge/lockforge/internal/events"
	"github.com/lockforge/lockforge/internal/fencing"
	"github.com/lockforge/lockforge/internal/lockmanager"
	"github.com/lockforge/lockforge/internal/protectedresource"
	"github.com/lockforge/lockforge/internal/redisadapter"
)

func setupTestManager(t *testing.T) (*lockmanager.LockManager, etcdclient.Client, *protectedresource.ProtectedResource) {
	client := etcdclient.NewMemoryClient()
	fencingGen := fencing.NewGenerator(client)
	redisAdapter := redisadapter.NewAdapter("")
	eventBuffer := events.NewRingBuffer(500)
	protectedRes := protectedresource.NewProtectedResource(eventBuffer)

	lm := lockmanager.NewLockManager(client, fencingGen, redisAdapter, eventBuffer, lockmanager.Config{
		DefaultTTL: 10,
	})

	t.Cleanup(func() {
		lm.Close()
		client.Close()
	})

	return lm, client, protectedRes
}

// 1. Multi-client concurrent acquisition test:
// 15 concurrent goroutine clients hammer the same resource. Exactly 1 must win.
func TestConcurrentAcquisition(t *testing.T) {
	lm, _, _ := setupTestManager(t)
	ctx := context.Background()

	resourceID := "shared-database-migration-lock"
	numClients := 15
	var wg sync.WaitGroup
	var mu sync.Mutex

	var winners []string
	var rejected []string

	for i := 1; i <= numClients; i++ {
		wg.Add(1)
		clientID := fmt.Sprintf("worker-%02d", i)
		go func(cID string) {
			defer wg.Done()
			resp, err := lm.AcquireLock(ctx, resourceID, cID, 15)
			if err != nil {
				t.Errorf("Unexpected error from client %s: %v", cID, err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if resp.Acquired {
				winners = append(winners, cID)
			} else {
				rejected = append(rejected, cID)
			}
		}(clientID)
	}

	wg.Wait()

	if len(winners) != 1 {
		t.Fatalf("SAFETY VIOLATION: Expected exactly 1 winner, got %d winners: %v", len(winners), winners)
	}
	if len(rejected) != numClients-1 {
		t.Fatalf("Expected %d rejected clients, got %d", numClients-1, len(rejected))
	}

	// Verify the winner actually holds the lock
	inspect, err := lm.InspectLock(ctx, resourceID)
	if err != nil {
		t.Fatalf("InspectLock failed: %v", err)
	}
	if !inspect.IsLocked || inspect.OwnerId != winners[0] {
		t.Fatalf("Inspect lock mismatch: expected owner %s, got %s", winners[0], inspect.OwnerId)
	}
}

// 2. Crash-and-recover test:
// Client acquires lock with short TTL and crashes/abandons it.
// Verify lease auto-expires and second client acquires with higher fencing token.
func TestLeaseExpiryAndRecovery(t *testing.T) {
	lm, _, _ := setupTestManager(t)
	ctx := context.Background()

	resourceID := "worker-job-lock"
	client1 := "client-alpha"
	client2 := "client-beta"

	// Client 1 acquires lock with 1s TTL
	resp1, err := lm.AcquireLock(ctx, resourceID, client1, 1)
	if err != nil || !resp1.Acquired {
		t.Fatalf("Client 1 failed to acquire: %v", err)
	}
	token1 := resp1.FencingToken

	// Client 2 immediately tries to acquire and fails
	resp2Initial, _ := lm.AcquireLock(ctx, resourceID, client2, 5)
	if resp2Initial.Acquired {
		t.Fatal("Client 2 should have been blocked while Client 1 holds lock")
	}

	// Wait for lease to expire (1.2s)
	time.Sleep(1200 * time.Millisecond)

	// Client 2 retries after lease expiry -> should succeed with strictly higher fencing token
	resp2, err := lm.AcquireLock(ctx, resourceID, client2, 5)
	if err != nil || !resp2.Acquired {
		t.Fatalf("Client 2 failed to recover lock after lease expiry: %v", err)
	}
	token2 := resp2.FencingToken

	if token2 <= token1 {
		t.Fatalf("Fencing token must strictly increase: token2 (%d) <= token1 (%d)", token2, token1)
	}
}

// 3. Stale-write rejection test end-to-end (Martin Kleppmann theorem):
// Old fencing token must be rejected by Protected Resource when a higher token has been processed.
func TestStaleWriteRejection(t *testing.T) {
	lm, _, protectedRes := setupTestManager(t)
	ctx := context.Background()

	resourceID := "bank-account-ledger"

	// 1. Client A acquires lock
	respA, err := lm.AcquireLock(ctx, resourceID, "client-A", 1)
	if err != nil || !respA.Acquired {
		t.Fatalf("Client A failed to acquire lock: %v", err)
	}
	tokenA := respA.FencingToken

	// 2. Client A experiences simulated GC pause / crash. Lease expires.
	time.Sleep(1200 * time.Millisecond)

	// 3. Client B acquires lock with new, higher token
	respB, err := lm.AcquireLock(ctx, resourceID, "client-B", 5)
	if err != nil || !respB.Acquired {
		t.Fatalf("Client B failed to acquire lock: %v", err)
	}
	tokenB := respB.FencingToken

	if tokenB <= tokenA {
		t.Fatalf("Expected tokenB (%d) > tokenA (%d)", tokenB, tokenA)
	}

	// 4. Client B writes to protected resource -> accepted
	writeB, err := protectedRes.Write(ctx, resourceID, "client-B", tokenB, "balance=$500")
	if err != nil || !writeB.Accepted {
		t.Fatalf("Client B write should have been accepted: %v", err)
	}

	// 5. Stale Client A wakes up from GC pause and attempts write with tokenA -> REJECTED!
	writeA, err := protectedRes.Write(ctx, resourceID, "client-A", tokenA, "balance=$100")
	if err == nil || writeA.Accepted {
		t.Fatalf("SAFETY CRITICAL FAILURE: Stale write with token %d was accepted when token %d was already processed!", tokenA, tokenB)
	}

	// Verify ledger state contains Client B's data, not stale Client A's data
	rec, ok := protectedRes.GetRecord(resourceID)
	if !ok || rec.Data != "balance=$500" {
		t.Fatalf("Ledger corrupted! Expected 'balance=$500', got '%v'", rec)
	}
}

// 4. Network-partition / Quorum loss test:
// Stop quorum, confirm no new locks are granted (prevents split-brain).
func TestNetworkPartitionQuorumSafety(t *testing.T) {
	lm, client, _ := setupTestManager(t)
	ctx := context.Background()

	resourceID := "critical-shared-resource"

	// Simulate minority partition / quorum loss
	client.SetPartitionMode(true)

	// Attempt acquire lock during partition -> must fail!
	resp, err := lm.AcquireLock(ctx, resourceID, "partitioned-client", 5)
	if resp != nil && resp.Acquired {
		t.Fatal("SPLIT-BRAIN RISK: Lock acquired when cluster lacked Raft quorum!")
	}
	if err == nil && resp.ErrorMessage == "" {
		t.Fatal("Expected error or acquisition failure during partition")
	}

	// Restore quorum
	client.SetPartitionMode(false)

	// Lock acquisition must now succeed
	respRestored, err := lm.AcquireLock(ctx, resourceID, "restored-client", 5)
	if err != nil || !respRestored.Acquired {
		t.Fatalf("Failed to acquire lock after quorum restored: %v", err)
	}
}

// 5. Lease renewal keep-alive test:
// Client renews lease before expiry; verify lock persists past original TTL.
func TestRenewLeaseHeartbeat(t *testing.T) {
	lm, _, _ := setupTestManager(t)
	ctx := context.Background()

	resourceID := "long-running-job"
	clientID := "worker-charlie"

	// Acquire lock with 2s TTL
	resp, err := lm.AcquireLock(ctx, resourceID, clientID, 2)
	if err != nil || !resp.Acquired {
		t.Fatalf("Failed to acquire: %v", err)
	}

	// Sleep 1 second (halfway through TTL)
	time.Sleep(1000 * time.Millisecond)

	// Renew lease for +3s
	renewResp, err := lm.RenewLease(ctx, resourceID, clientID, resp.LeaseId, 3)
	if err != nil || !renewResp.Renewed {
		t.Fatalf("Failed to renew lease: %v", err)
	}

	// Sleep another 1.5 seconds (2.5s since start, would have expired without renewal)
	time.Sleep(1500 * time.Millisecond)

	// Verify lock is still held by clientID
	inspect, err := lm.InspectLock(ctx, resourceID)
	if err != nil || !inspect.IsLocked || inspect.OwnerId != clientID {
		t.Fatalf("Lock should still be active due to renewal, inspect: %+v", inspect)
	}

	// Now explicitly release
	rel, err := lm.ReleaseLock(ctx, resourceID, clientID, resp.LeaseId, resp.FencingToken)
	if err != nil || !rel.Released {
		t.Fatalf("Failed to release lock: %v", err)
	}
}
