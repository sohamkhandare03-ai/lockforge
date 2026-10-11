package tests

import (
	"context"
	"sync"
	"testing"

	"github.com/lockforge/lockforge/internal/etcdclient"
	"github.com/lockforge/lockforge/internal/events"
	"github.com/lockforge/lockforge/internal/fencing"
	"github.com/lockforge/lockforge/internal/protectedresource"
)

func TestFencingTokenStrictMonotonicity(t *testing.T) {
	client := etcdclient.NewMemoryClient()
	defer client.Close()

	gen := fencing.NewGenerator(client)
	ctx := context.Background()

	resourceID := "fencing-counter-test"
	var prevToken int64 = 0

	for i := 0; i < 50; i++ {
		token, err := gen.NextToken(ctx, resourceID)
		if err != nil {
			t.Fatalf("Failed to get next token: %v", err)
		}
		if token <= prevToken {
			t.Fatalf("Monotonicity violation: current token %d <= previous token %d", token, prevToken)
		}
		prevToken = token
	}
}

func TestConcurrentFencingTokenGeneration(t *testing.T) {
	client := etcdclient.NewMemoryClient()
	defer client.Close()

	gen := fencing.NewGenerator(client)
	ctx := context.Background()

	resourceID := "concurrent-tokens"
	numRoutines := 20
	tokens := make(chan int64, numRoutines)
	var wg sync.WaitGroup

	for i := 0; i < numRoutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tok, err := gen.NextToken(ctx, resourceID)
			if err == nil {
				tokens <- tok
			}
		}()
	}

	wg.Wait()
	close(tokens)

	seen := make(map[int64]bool)
	for tok := range tokens {
		if seen[tok] {
			t.Fatalf("Duplicate token issued: %d", tok)
		}
		seen[tok] = true
	}
}

func TestProtectedResourceStaleWriteRejection(t *testing.T) {
	eventBuffer := events.NewRingBuffer(100)
	pr := protectedresource.NewProtectedResource(eventBuffer)
	ctx := context.Background()

	resID := "bank-account-123"

	// Write 1: Token 101 -> Accepted
	res1, err := pr.Write(ctx, resID, "worker-1", 101, "deposit 100")
	if err != nil || !res1.Accepted {
		t.Fatalf("Write 1 should have been accepted: %v", err)
	}

	// Write 2: Token 105 -> Accepted
	res2, err := pr.Write(ctx, resID, "worker-2", 105, "deposit 200")
	if err != nil || !res2.Accepted {
		t.Fatalf("Write 2 should have been accepted: %v", err)
	}

	// Write 3: Stale Token 102 (< 105) -> REJECTED
	res3, err := pr.Write(ctx, resID, "stale-worker", 102, "unauthorized withdraw")
	if err == nil || res3.Accepted {
		t.Fatalf("Stale write with token 102 must be rejected! Result: %+v", res3)
	}
	if err != protectedresource.ErrStaleWriteRejected {
		t.Fatalf("Expected ErrStaleWriteRejected, got: %v", err)
	}

	// Write 4: Equal Token 105 (<= 105) -> REJECTED
	res4, err := pr.Write(ctx, resID, "replay-worker", 105, "replay attack")
	if err == nil || res4.Accepted {
		t.Fatalf("Replayed write with equal token 105 must be rejected! Result: %+v", res4)
	}

	// Verify state remained deposit 200
	rec, ok := pr.GetRecord(resID)
	if !ok || rec.Data != "deposit 200" || rec.LastSeenToken != 105 {
		t.Fatalf("State corrupted! Record: %+v", rec)
	}
}
