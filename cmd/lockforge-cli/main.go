package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	pb "github.com/lockforge/lockforge/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	defaultGRPCAddr = "localhost:50051"
	defaultHTTPAddr = "http://localhost:8080"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]
	switch command {
	case "acquire":
		runAcquire(os.Args[2:])
	case "release":
		runRelease(os.Args[2:])
	case "renew":
		runRenew(os.Args[2:])
	case "heartbeat":
		runHeartbeat(os.Args[2:])
	case "inspect":
		runInspect(os.Args[2:])
	case "list":
		runList(os.Args[2:])
	case "write":
		runWrite(os.Args[2:])
	case "chaos":
		runChaos(os.Args[2:])
	case "benchmark":
		runBenchmark(os.Args[2:])
	case "help":
		printUsage()
	default:
		fmt.Printf("Unknown command '%s'\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`LockForge CLI – Distributed Lock Manager Control Tool

Usage:
  lockforge-cli <command> [arguments]

Commands:
  acquire    Acquire a distributed lock with lease TTL & fencing token
  release    Release a held lock
  renew      Extend lease TTL (keep-alive)
  heartbeat  Acquire lock and run continuous background lease heartbeat loop
  inspect    Check current owner and TTL remaining
  list       List all active locks across the cluster
  write      Submit data to Protected Resource validating fencing token
  chaos      Execute chaos test scenarios (concurrency | crash | stale_write | partition)
  benchmark  Run performance comparison benchmark (etcd Raft vs Redis)
  help       Show this help message

Examples:
  lockforge-cli acquire -resource order-101 -client worker-alpha -ttl 10
  lockforge-cli heartbeat -resource order-101 -client worker-alpha -ttl 6 -interval 2
  lockforge-cli inspect -resource order-101
  lockforge-cli write -resource order-101 -client worker-alpha -token 101 -data "paid=true"
  lockforge-cli release -resource order-101 -client worker-alpha
  lockforge-cli chaos stale_write
  lockforge-cli benchmark`)
}

func getGRPCClient(addr string) (pb.LockServiceClient, *grpc.ClientConn, error) {
	if addr == "" {
		addr = defaultGRPCAddr
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, nil, fmt.Errorf("could not connect to gRPC server at %s: %w", addr, err)
	}
	return pb.NewLockServiceClient(conn), conn, nil
}

func runAcquire(args []string) {
	fs := flag.NewFlagSet("acquire", flag.ExitOnError)
	resID := fs.String("resource", "", "Resource identifier")
	clientID := fs.String("client", "cli-worker", "Client identifier")
	ttl := fs.Int64("ttl", 10, "Lease TTL in seconds")
	grpcAddr := fs.String("server", defaultGRPCAddr, "gRPC server address")
	_ = fs.Parse(args)

	if *resID == "" {
		fmt.Println("Error: -resource flag is required")
		os.Exit(1)
	}

	client, conn, err := getGRPCClient(*grpcAddr)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	resp, err := client.AcquireLock(ctx, &pb.AcquireLockRequest{
		ResourceId: *resID,
		ClientId:   *clientID,
		TtlSeconds: *ttl,
	})
	if err != nil {
		fmt.Printf("RPC Failed: %v\n", err)
		os.Exit(1)
	}

	if resp.Acquired {
		fmt.Println("------------------------------------------------------------")
		fmt.Println("LOCK ACQUIRED SUCCESSFULLY!")
		fmt.Println("------------------------------------------------------------")
		fmt.Printf("  Resource:       %s\n", resp.ResourceId)
		fmt.Printf("  Owner:          %s\n", resp.ClientId)
		fmt.Printf("  Fencing Token:  %d (Monotonic guard against stale writes)\n", resp.FencingToken)
		fmt.Printf("  Lease ID:       %d\n", resp.LeaseId)
		fmt.Printf("  TTL:            %ds (Expires at: %s)\n", *ttl, time.UnixMilli(resp.ExpiresAtUnixMs).Format(time.RFC3339))
		fmt.Println("------------------------------------------------------------")
	} else {
		fmt.Println("------------------------------------------------------------")
		fmt.Println("LOCK ACQUISITION FAILED (Already held)")
		fmt.Println("------------------------------------------------------------")
		fmt.Printf("  Resource:       %s\n", resp.ResourceId)
		fmt.Printf("  Current Holder: %s\n", resp.CurrentOwnerId)
		fmt.Printf("  Reason:         %s\n", resp.ErrorMessage)
		fmt.Println("------------------------------------------------------------")
	}
}

func runRelease(args []string) {
	fs := flag.NewFlagSet("release", flag.ExitOnError)
	resID := fs.String("resource", "", "Resource identifier")
	clientID := fs.String("client", "", "Client identifier (owner)")
	leaseID := fs.Int64("lease", 0, "Lease ID")
	token := fs.Int64("token", 0, "Fencing token")
	grpcAddr := fs.String("server", defaultGRPCAddr, "gRPC server address")
	_ = fs.Parse(args)

	if *resID == "" {
		fmt.Println("Error: -resource flag is required")
		os.Exit(1)
	}

	client, conn, err := getGRPCClient(*grpcAddr)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	resp, err := client.ReleaseLock(ctx, &pb.ReleaseLockRequest{
		ResourceId:   *resID,
		ClientId:     *clientID,
		LeaseId:      *leaseID,
		FencingToken: *token,
	})
	if err != nil {
		fmt.Printf("RPC Failed: %v\n", err)
		os.Exit(1)
	}

	if resp.Released {
		fmt.Printf("Lock released on '%s': %s\n", resp.ResourceId, resp.Message)
	} else {
		fmt.Printf("Failed to release lock on '%s': %s\n", resp.ResourceId, resp.Message)
	}
}

func runRenew(args []string) {
	fs := flag.NewFlagSet("renew", flag.ExitOnError)
	resID := fs.String("resource", "", "Resource identifier")
	clientID := fs.String("client", "", "Client identifier")
	ttl := fs.Int64("ttl", 10, "Extension TTL in seconds")
	grpcAddr := fs.String("server", defaultGRPCAddr, "gRPC server address")
	_ = fs.Parse(args)

	if *resID == "" {
		fmt.Println("Error: -resource flag is required")
		os.Exit(1)
	}

	client, conn, err := getGRPCClient(*grpcAddr)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	resp, err := client.RenewLease(ctx, &pb.RenewLeaseRequest{
		ResourceId: *resID,
		ClientId:   *clientID,
		TtlSeconds: *ttl,
	})
	if err != nil {
		fmt.Printf("RPC Failed: %v\n", err)
		os.Exit(1)
	}

	if resp.Renewed {
		fmt.Printf("Lease renewed for '%s'. New expiration: %s\n", resp.ResourceId, time.UnixMilli(resp.NewExpiresAtUnixMs).Format(time.RFC3339))
	} else {
		fmt.Printf("Failed to renew lease for '%s': %s\n", resp.ResourceId, resp.Message)
	}
}

func runHeartbeat(args []string) {
	fs := flag.NewFlagSet("heartbeat", flag.ExitOnError)
	resID := fs.String("resource", "", "Resource identifier")
	clientID := fs.String("client", "heartbeat-worker", "Client identifier")
	ttl := fs.Int64("ttl", 6, "Lease TTL in seconds")
	interval := fs.Int64("interval", 2, "Heartbeat renewal interval in seconds")
	grpcAddr := fs.String("server", defaultGRPCAddr, "gRPC server address")
	_ = fs.Parse(args)

	if *resID == "" {
		fmt.Println("Error: -resource flag is required")
		os.Exit(1)
	}

	client, conn, err := getGRPCClient(*grpcAddr)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	resp, err := client.AcquireLock(ctx, &pb.AcquireLockRequest{
		ResourceId: *resID,
		ClientId:   *clientID,
		TtlSeconds: *ttl,
	})
	cancel()
	if err != nil || !resp.Acquired {
		errMsg := "unknown error"
		if resp != nil {
			errMsg = resp.ErrorMessage
		} else if err != nil {
			errMsg = err.Error()
		}
		fmt.Printf("Failed to acquire initial lock: %s\n", errMsg)
		os.Exit(1)
	}

	fmt.Println("------------------------------------------------------------")
	fmt.Println("HEARTBEAT SESSION STARTED")
	fmt.Println("------------------------------------------------------------")
	fmt.Printf("  Resource:       %s\n", resp.ResourceId)
	fmt.Printf("  Owner:          %s\n", resp.ClientId)
	fmt.Printf("  Fencing Token:  %d (Monotonic sequence)\n", resp.FencingToken)
	fmt.Printf("  Lease ID:       %d\n", resp.LeaseId)
	fmt.Printf("  TTL:            %ds (Interval: %ds)\n", *ttl, *interval)
	fmt.Println("------------------------------------------------------------")
	fmt.Println("LockForge client actively renewing lease. Press Ctrl+C to kill client and verify TTL expiry.")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	ticker := time.NewTicker(time.Duration(*interval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-sigChan:
			fmt.Printf("\n[SIMULATED CRASH] Client terminated! Heartbeats halted.\nNotice: Lease will auto-expire in <= %ds and allow failover.\n", *ttl)
			return
		case t := <-ticker.C:
			renewCtx, renewCancel := context.WithTimeout(context.Background(), 2*time.Second)
			renewResp, rErr := client.RenewLease(renewCtx, &pb.RenewLeaseRequest{
				ResourceId: *resID,
				ClientId:   *clientID,
				LeaseId:    resp.LeaseId,
				TtlSeconds: *ttl,
			})
			renewCancel()
			if rErr != nil || !renewResp.Renewed {
				fmt.Printf("[%s] Heartbeat renewal FAILED: %v\n", t.Format("15:04:05"), rErr)
				return
			}
			fmt.Printf("[%s] Heartbeat ping sent: lease renewed (+%ds, expires at: %s)\n",
				t.Format("15:04:05"), *ttl, time.UnixMilli(renewResp.NewExpiresAtUnixMs).Format("15:04:05"))
		}
	}
}

func runInspect(args []string) {
	fs := flag.NewFlagSet("inspect", flag.ExitOnError)
	resID := fs.String("resource", "", "Resource identifier")
	grpcAddr := fs.String("server", defaultGRPCAddr, "gRPC server address")
	_ = fs.Parse(args)

	if *resID == "" {
		fmt.Println("Error: -resource flag is required")
		os.Exit(1)
	}

	client, conn, err := getGRPCClient(*grpcAddr)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	resp, err := client.InspectLock(ctx, &pb.InspectLockRequest{
		ResourceId: *resID,
	})
	if err != nil {
		fmt.Printf("RPC Failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("------------------------------------------------------------")
	fmt.Printf("LOCK INSPECTION: %s\n", resp.ResourceId)
	fmt.Println("------------------------------------------------------------")
	fmt.Printf("  Locked:          %t\n", resp.IsLocked)
	if resp.IsLocked {
		fmt.Printf("  Owner:           %s\n", resp.OwnerId)
		fmt.Printf("  Fencing Token:   %d\n", resp.FencingToken)
		fmt.Printf("  Lease ID:        %d\n", resp.LeaseId)
		fmt.Printf("  TTL Remaining:   %ds\n", resp.TtlRemainingSeconds)
		fmt.Printf("  Acquired At:     %s\n", time.UnixMilli(resp.AcquiredAtUnixMs).Format(time.RFC3339))
		fmt.Printf("  Expires At:      %s\n", time.UnixMilli(resp.ExpiresAtUnixMs).Format(time.RFC3339))
	} else {
		fmt.Println("  Status:          Available (No active lease)")
	}
	fmt.Println("------------------------------------------------------------")
}

func runList(args []string) {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	grpcAddr := fs.String("server", defaultGRPCAddr, "gRPC server address")
	_ = fs.Parse(args)

	client, conn, err := getGRPCClient(*grpcAddr)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	resp, err := client.GetActiveLocks(ctx, &pb.GetActiveLocksRequest{})
	if err != nil {
		fmt.Printf("RPC Failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Total Active Locks: %d\n\n", len(resp.Locks))
	if len(resp.Locks) == 0 {
		fmt.Println("No active locks currently held in the cluster.")
		return
	}

	fmt.Printf("%-24s %-16s %-10s %-14s %-12s\n", "RESOURCE ID", "OWNER", "TOKEN", "LEASE ID", "TTL (s)")
	fmt.Println(strings.Repeat("-", 76))
	now := time.Now().UnixMilli()
	for _, l := range resp.Locks {
		rem := (l.ExpiresAtUnixMs - now) / 1000
		if rem < 0 {
			rem = 0
		}
		fmt.Printf("%-24s %-16s %-10d %-14d %-12d\n", l.ResourceId, l.OwnerId, l.FencingToken, l.LeaseId, rem)
	}
}

func runWrite(args []string) {
	fs := flag.NewFlagSet("write", flag.ExitOnError)
	resID := fs.String("resource", "", "Resource identifier")
	clientID := fs.String("client", "worker", "Client identifier")
	token := fs.Int64("token", 0, "Fencing token")
	data := fs.String("data", "sample_payload", "Data to write")
	httpAddr := fs.String("http", defaultHTTPAddr, "HTTP API address")
	_ = fs.Parse(args)

	if *resID == "" || *token == 0 {
		fmt.Println("Error: -resource and -token are required")
		os.Exit(1)
	}

	reqBody, _ := json.Marshal(map[string]any{
		"resource_id":   *resID,
		"client_id":     *clientID,
		"fencing_token": *token,
		"data":          *data,
	})

	resp, err := http.Post(*httpAddr+"/api/resource/write", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		fmt.Printf("HTTP request failed: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var res map[string]any
	_ = json.Unmarshal(body, &res)

	fmt.Println("------------------------------------------------------------")
	if resp.StatusCode == http.StatusOK {
		fmt.Println("WRITE ACCEPTED BY PROTECTED RESOURCE!")
		fmt.Printf("  Resource:        %s\n", *resID)
		fmt.Printf("  Client:          %s\n", *clientID)
		fmt.Printf("  Fencing Token:   %d\n", *token)
		fmt.Printf("  Server Message:  %s\n", res["message"])
	} else {
		fmt.Println("WRITE REJECTED! (FENCING TOKEN VIOLATION)")
		fmt.Printf("  Resource:        %s\n", *resID)
		fmt.Printf("  Incoming Token:  %d\n", *token)
		fmt.Printf("  Last Seen Token: %v\n", res["last_seen_token"])
		fmt.Printf("  Rejection Note:  %s\n", res["message"])
	}
	fmt.Println("------------------------------------------------------------")
}

func runChaos(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: lockforge-cli chaos <concurrency | crash | stale_write | partition>")
		os.Exit(1)
	}
	scenario := args[0]
	resp, err := http.Post(defaultHTTPAddr+"/api/chaos/"+scenario, "application/json", nil)
	if err != nil {
		fmt.Printf("Chaos trigger failed: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var formatted bytes.Buffer
	_ = json.Indent(&formatted, body, "", "  ")
	fmt.Println(formatted.String())
}

func runBenchmark(args []string) {
	fmt.Println("Executing LockForge performance benchmark (etcd Raft vs Redis Read Cache)...")
	resp, err := http.Get(defaultHTTPAddr + "/api/benchmark")
	if err != nil {
		fmt.Printf("Benchmark request failed: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var res map[string]any
	_ = json.Unmarshal(body, &res)

	etcdMap, _ := res["etcd_raft"].(map[string]any)
	redisMap, _ := res["redis_cache"].(map[string]any)

	fmt.Println("\n==========================================================================")
	fmt.Println("         LOCKFORGE PERFORMANCE BENCHMARK RESULTS                         ")
	fmt.Println("==========================================================================")
	fmt.Printf("%-22s %-12s %-14s %-12s %-20s\n", "ENGINE", "THROUGHPUT", "AVG LATENCY", "P95 LATENCY", "CONSISTENCY MODEL")
	fmt.Println(strings.Repeat("-", 80))
	fmt.Printf("%-22s %-12.1f ops/s %-14.2f ms %-12.2f ms %-20s\n",
		"etcd (Raft Consensus)",
		etcdMap["ops_per_sec"],
		etcdMap["avg_lat_ms"],
		etcdMap["p95_lat_ms"],
		etcdMap["consistency"],
	)
	fmt.Printf("%-22s %-12.1f ops/s %-14.2f ms %-12.2f ms %-20s\n",
		"Redis (Read Shortcut)",
		redisMap["ops_per_sec"],
		redisMap["avg_lat_ms"],
		redisMap["p95_lat_ms"],
		redisMap["consistency"],
	)
	fmt.Println("==========================================================================")
	fmt.Println("Summary: etcd provides strict linearizability (Martin Kleppmann safe);")
	fmt.Println("         Redis read cache accelerates non-consensus queries with sub-ms reads.")
}
