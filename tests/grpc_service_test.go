package tests

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/lockforge/lockforge/internal/etcdclient"
	"github.com/lockforge/lockforge/internal/events"
	"github.com/lockforge/lockforge/internal/fencing"
	"github.com/lockforge/lockforge/internal/grpcserver"
	"github.com/lockforge/lockforge/internal/lockmanager"
	"github.com/lockforge/lockforge/internal/redisadapter"
	pb "github.com/lockforge/lockforge/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

const bufSize = 1024 * 1024

func setupGRPCTest(t *testing.T) (pb.LockServiceClient, *lockmanager.LockManager, func()) {
	client := etcdclient.NewMemoryClient()
	fencingGen := fencing.NewGenerator(client)
	redisAdapter := redisadapter.NewAdapter("")
	eventBuffer := events.NewRingBuffer(500)

	lm := lockmanager.NewLockManager(client, fencingGen, redisAdapter, eventBuffer, lockmanager.Config{
		DefaultTTL: 10,
	})

	srv := grpcserver.NewServer(lm, eventBuffer)

	lis := bufconn.Listen(bufSize)
	baseServer := grpc.NewServer()
	pb.RegisterLockServiceServer(baseServer, srv)

	go func() {
		if err := baseServer.Serve(lis); err != nil && err != grpc.ErrServerStopped {
			// ignore stopped error
		}
	}()

	bufDialer := func(context.Context, string) (net.Conn, error) {
		return lis.Dial()
	}

	conn, err := grpc.DialContext(
		context.Background(),
		"bufnet",
		grpc.WithContextDialer(bufDialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("Failed to dial bufnet: %v", err)
	}

	grpcClient := pb.NewLockServiceClient(conn)

	cleanup := func() {
		conn.Close()
		baseServer.GracefulStop()
		lis.Close()
		lm.Close()
		client.Close()
	}

	return grpcClient, lm, cleanup
}

func TestGRPCAcquireAndRelease(t *testing.T) {
	client, _, cleanup := setupGRPCTest(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	resID := "grpc-test-resource-1"
	clientID := "grpc-worker-1"

	// 1. Acquire Lock
	acqResp, err := client.AcquireLock(ctx, &pb.AcquireLockRequest{
		ResourceId: resID,
		ClientId:   clientID,
		TtlSeconds: 10,
	})
	if err != nil {
		t.Fatalf("AcquireLock RPC failed: %v", err)
	}
	if !acqResp.Acquired {
		t.Fatalf("Expected lock to be acquired, got error: %s", acqResp.ErrorMessage)
	}
	if acqResp.FencingToken <= 0 {
		t.Fatalf("Expected positive fencing token, got: %d", acqResp.FencingToken)
	}

	// 2. Inspect Lock
	inspResp, err := client.InspectLock(ctx, &pb.InspectLockRequest{
		ResourceId: resID,
	})
	if err != nil {
		t.Fatalf("InspectLock RPC failed: %v", err)
	}
	if !inspResp.IsLocked || inspResp.OwnerId != clientID {
		t.Fatalf("InspectLock mismatch: isLocked=%v, owner=%s", inspResp.IsLocked, inspResp.OwnerId)
	}
	if inspResp.FencingToken != acqResp.FencingToken {
		t.Fatalf("Token mismatch: expected %d, got %d", acqResp.FencingToken, inspResp.FencingToken)
	}

	// 3. Second client attempts to acquire same resource -> fails
	acq2, err := client.AcquireLock(ctx, &pb.AcquireLockRequest{
		ResourceId: resID,
		ClientId:   "grpc-worker-2",
		TtlSeconds: 10,
	})
	if err != nil {
		t.Fatalf("AcquireLock RPC failed: %v", err)
	}
	if acq2.Acquired {
		t.Fatalf("Second client should not acquire lock held by %s", clientID)
	}

	// 4. Release Lock
	relResp, err := client.ReleaseLock(ctx, &pb.ReleaseLockRequest{
		ResourceId:   resID,
		ClientId:     clientID,
		LeaseId:      acqResp.LeaseId,
		FencingToken: acqResp.FencingToken,
	})
	if err != nil {
		t.Fatalf("ReleaseLock RPC failed: %v", err)
	}
	if !relResp.Released {
		t.Fatalf("Expected lock to be released, got message: %s", relResp.Message)
	}

	// 5. Inspect after release
	inspAfter, err := client.InspectLock(ctx, &pb.InspectLockRequest{
		ResourceId: resID,
	})
	if err != nil {
		t.Fatalf("InspectLock RPC failed: %v", err)
	}
	if inspAfter.IsLocked {
		t.Fatal("Expected lock to be unlocked after release")
	}
}

func TestGRPCRenewLease(t *testing.T) {
	client, _, cleanup := setupGRPCTest(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	resID := "grpc-renew-resource"
	clientID := "grpc-worker-renew"

	acqResp, err := client.AcquireLock(ctx, &pb.AcquireLockRequest{
		ResourceId: resID,
		ClientId:   clientID,
		TtlSeconds: 5,
	})
	if err != nil || !acqResp.Acquired {
		t.Fatalf("Failed to acquire: %v", err)
	}

	renewResp, err := client.RenewLease(ctx, &pb.RenewLeaseRequest{
		ResourceId: resID,
		ClientId:   clientID,
		LeaseId:    acqResp.LeaseId,
		TtlSeconds: 15,
	})
	if err != nil {
		t.Fatalf("RenewLease RPC failed: %v", err)
	}
	if !renewResp.Renewed {
		t.Fatalf("Expected lease to be renewed: %s", renewResp.Message)
	}
}

func TestGRPCGetActiveLocksAndStream(t *testing.T) {
	client, _, cleanup := setupGRPCTest(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := client.StreamUpdates(ctx, &pb.StreamUpdatesRequest{})
	if err != nil {
		t.Fatalf("StreamUpdates failed: %v", err)
	}

	evChan := make(chan *pb.LockEvent, 1)
	errChan := make(chan error, 1)
	go func() {
		ev, rErr := stream.Recv()
		if rErr != nil {
			errChan <- rErr
			return
		}
		evChan <- ev
	}()

	// Small pause to allow subscription to establish
	time.Sleep(50 * time.Millisecond)

	// Acquire lock to trigger event
	acqResp, err := client.AcquireLock(ctx, &pb.AcquireLockRequest{
		ResourceId: "stream-resource",
		ClientId:   "stream-client",
		TtlSeconds: 10,
	})
	if err != nil || !acqResp.Acquired {
		t.Fatalf("AcquireLock failed: %v", err)
	}

	// Verify active locks list
	activeResp, err := client.GetActiveLocks(ctx, &pb.GetActiveLocksRequest{})
	if err != nil {
		t.Fatalf("GetActiveLocks failed: %v", err)
	}
	if len(activeResp.Locks) == 0 {
		t.Fatal("Expected at least 1 active lock in list")
	}

	// Read event from stream with timeout
	select {
	case ev := <-evChan:
		if ev == nil || ev.ResourceId != "stream-resource" {
			t.Fatalf("Unexpected event received: %+v", ev)
		}
	case sErr := <-errChan:
		t.Fatalf("Stream error: %v", sErr)
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for lock event on gRPC stream")
	}
}
