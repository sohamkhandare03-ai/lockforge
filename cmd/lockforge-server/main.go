package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/lockforge/lockforge/internal/dashboard"
	"github.com/lockforge/lockforge/internal/etcdclient"
	"github.com/lockforge/lockforge/internal/events"
	"github.com/lockforge/lockforge/internal/fencing"
	"github.com/lockforge/lockforge/internal/grpcserver"
	"github.com/lockforge/lockforge/internal/lockmanager"
	"github.com/lockforge/lockforge/internal/protectedresource"
	"github.com/lockforge/lockforge/internal/redisadapter"
	pb "github.com/lockforge/lockforge/proto"
)

func main() {
	grpcPort := flag.Int("grpc-port", 50051, "Port for gRPC service")
	httpPort := flag.Int("http-port", 8080, "Port for HTTP dashboard & SSE API")
	etcdEndpoints := flag.String("etcd-endpoints", "", "Comma-separated etcd endpoints (e.g. localhost:2379)")
	redisAddr := flag.String("redis-addr", "", "Redis host:port (e.g. localhost:6379)")
	useMemory := flag.Bool("use-memory", false, "Force in-memory Raft simulation backend")
	flag.Parse()

	// Read environment overrides
	if envPort := os.Getenv("PORT"); envPort != "" {
		if p, err := strconv.Atoi(envPort); err == nil {
			*httpPort = p
		}
	} else if envHTTP := os.Getenv("HTTP_PORT"); envHTTP != "" {
		if p, err := strconv.Atoi(envHTTP); err == nil {
			*httpPort = p
		}
	}
	if envGRPC := os.Getenv("GRPC_PORT"); envGRPC != "" {
		if p, err := strconv.Atoi(envGRPC); err == nil {
			*grpcPort = p
		}
	}
	if envEndpoints := os.Getenv("ETCD_ENDPOINTS"); envEndpoints != "" && *etcdEndpoints == "" {
		*etcdEndpoints = envEndpoints
	}
	if envRedis := os.Getenv("REDIS_ADDR"); envRedis != "" && *redisAddr == "" {
		*redisAddr = envRedis
	}

	log.Println("================================================================")
	log.Println("     LockForge – Distributed Lock Manager (Go + gRPC + Raft)    ")
	log.Println("================================================================")

	var endpoints []string
	if *etcdEndpoints != "" {
		endpoints = strings.Split(*etcdEndpoints, ",")
	}

	// 1. Initialize etcd client
	etcdCfg := etcdclient.Config{
		Endpoints:   endpoints,
		DialTimeout: 2 * time.Second,
		UseMemory:   *useMemory,
	}
	client, err := etcdclient.NewClient(etcdCfg)
	if err != nil {
		log.Fatalf("Failed to initialize etcd client: %v", err)
	}
	defer client.Close()
	log.Printf("Storage engine initialized (Endpoints: %v, Mode: %s)", endpoints, func() string {
		if *useMemory || len(endpoints) == 0 {
			return "In-Memory Raft/MVCC Simulation"
		}
		return "Real etcd cluster"
	}())

	// 2. Initialize Redis Adapter
	redisAdapter := redisadapter.NewAdapter(*redisAddr)
	log.Printf("Redis cache adapter initialized (Target: '%s')", *redisAddr)

	// 3. Initialize Event Buffer & Fencing Generator
	eventBuffer := events.NewRingBuffer(1000)
	fencingGen := fencing.NewGenerator(client)

	// 4. Initialize LockManager
	lm := lockmanager.NewLockManager(client, fencingGen, redisAdapter, eventBuffer, lockmanager.Config{
		DefaultTTL: 10,
	})
	defer lm.Close()

	// 5. Initialize Simulated Protected Resource
	protectedRes := protectedresource.NewProtectedResource(eventBuffer)

	// 6. Start gRPC Server
	grpcSrv := grpcserver.NewServer(lm, eventBuffer)
	go func() {
		log.Printf("Starting gRPC server on :%d ...", *grpcPort)
		if err := grpcSrv.Start(*grpcPort); err != nil {
			log.Printf("gRPC server error: %v", err)
		}
	}()

	// 7. Start HTTP / SSE Dashboard Gateway
	dashHandler := dashboard.NewHandler(lm, eventBuffer, protectedRes)
	mux := http.NewServeMux()
	dashHandler.RegisterRoutes(mux)

	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", *httpPort),
		Handler: mux,
	}

	go func() {
		log.Printf("Starting HTTP Dashboard & SSE Gateway on :%d ...", *httpPort)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("HTTP server error: %v", err)
		}
	}()

	// Log initial system event
	eventBuffer.Publish(
		pb.LockEventType_LOCK_EVENT_TYPE_UNSPECIFIED,
		"cluster",
		"system",
		0,
		0,
		fmt.Sprintf("LockForge cluster online (gRPC: %d, HTTP: %d)", *grpcPort, *httpPort),
	)

	log.Printf("LockForge initialized successfully. Ready to accept lock requests.")

	// Graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down LockForge...")
	grpcSrv.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(ctx)
	log.Println("LockForge server exited cleanly.")
}
