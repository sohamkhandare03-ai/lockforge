# LockForge – Distributed Lock Manager

> **High-Assurance Distributed Lock Manager with Raft Consensus, Lease Management, and Fencing Tokens.**  
> *Prepared for: T.Y.B.Sc. Computer Science Mini Project — Roll No. CS-9044*

[![Go Test](https://img.shields.io/badge/Tests-13%20Passed-brightgreen.svg)]()
[![Go Race](https://img.shields.io/badge/Race%20Detector-Zero%20Data%20Races-blue.svg)]()
[![gRPC](https://img.shields.io/badge/gRPC-v1.83-blue.svg)]()
[![Protobuf](https://img.shields.io/badge/protobuf-v3-orange.svg)]()
[![React](https://img.shields.io/badge/React-19.0-61dafb.svg)]()
[![TypeScript](https://img.shields.io/badge/TypeScript-5.7-3178c6.svg)]()

---

## Architecture Overview

```
                      +---------------------------------------+
                      | React + TypeScript Dashboard (Web UI)  |
                      |  - Active locks table                 |
                      |  - Live TTL countdown                 |
                      |  - Interactive Fencing Token Demo     |
                      +-------------------+-------------------+
                                          | HTTP REST (port 8080)
                                          v
+------------------+             +-------------------------------+             +-----------------------+
|   gRPC Clients   |  gRPC:50051 |   LockForge Gateway & Engine  |             | Mock Protected        |
|  (CLI / Workers) | ----------->|  - LockManager (CAS Txn)      |             | Resource (Data Store) |
+------------------+             |  - Lease / Heartbeat Loop     |             |  - Rejects writes     |
                                 |  - Fencing Token Generator    |             |    with stale token   |
                                 |  - Redis Read Cache           |             +-----------------------+
                                 +---------------+---------------+                         ^
                                                 |                                         |
                                    +------------+------------+                            |
                                    |                         |                            |
                                    v                         v                            |
                             +--------------+          +--------------+                    |
                             |  etcd Cluster|          |  Redis Cache |                    |
                             |  (Raft CAS,  |          | (InspectLock |                    |
                             |   Leases,    |          |  Status Read |                    |
                             |   Tokens)    |          |    Cache)    |                    |
                             +--------------+          +--------------+                    |
                                    |                                                      |
                                    +------------------------------------------------------+
```

LockForge solves the fundamental limitation of naive distributed locks (such as Redis `SETNX`): **split-brain dual writes caused by process pauses (GC stalls, CPU starvation) and network partitions**.

---

## Core Features

1. **Raft Consensus (etcd v3 MVCC)**:
   - Atomic Compare-And-Swap (`Txn`) guarantees linearizable mutual exclusion.
   - Only 1 client can ever acquire a lock on a resource at any instant.
2. **Lease Management & Keep-Alive**:
   - Time-To-Live (TTL) automatically reclaims locks when a client crashes.
   - Asynchronous heartbeat loops (`lockforge-cli heartbeat`) extend leases.
3. **Monotonic Fencing Tokens (Martin Kleppmann Guard)**:
   - Every lock acquisition issues a strictly monotonically increasing token.
   - Storage engines reject writes with older tokens, preventing stale data corruption.
4. **Redis Read-Through Caching**:
   - Sub-millisecond reads for high-frequency lock inspection.
   - Invalidation upon explicit release or lease expiration.
5. **Interactive React + TypeScript Dashboard**:
   - Live TTL countdown bars, active locks table, real-time fencing progression timeline, and interactive chaos testing studio.
   - Automatically served directly by the backend at `http://localhost:8080`.

---

## Quick Start (Run Locally in 1 Command)

### 1. Start LockForge
```bash
cd /Users/siddhijadhav/.gemini/antigravity/scratch/lockforge
./start.sh
```
*(Or simply `go run ./cmd/lockforge-server`)*

This starts the LockForge server and automatically opens:
👉 **`http://localhost:8080`** in your browser!

The complete React + TypeScript dashboard is **embedded directly inside the binary**, requiring zero external web servers or node modules at runtime.

---

## Running the Automated Test Suites

```bash
# Run all 13 unit, integration, and gRPC tests
go test -v -count=1 ./tests/...

# Run all tests with Go Race Detector
go test -v -count=1 -race ./tests/...
```

### Running Chaos Test Suite
```bash
bash scripts/chaos.sh
```
Executes:
1. 15-client concurrent acquisition race.
2. Client crash & auto-recovery.
3. Martin Kleppmann stale-write rejection.
4. Network partition & Raft quorum loss safety.

### Running Performance Benchmark
```bash
./lockforge-benchmark -c 10 -n 200
```
Compares etcd linearizable Raft transactions vs Redis in-memory cache operations.

---

## CLI Usage Guide

```bash
# Acquire a lock with a 15-second lease
./lockforge-cli acquire -resource order-101 -client worker-alpha -ttl 15

# Inspect active lock status
./lockforge-cli inspect -resource order-101

# Start a continuous client heartbeat loop (press Ctrl+C to simulate crash)
./lockforge-cli heartbeat -resource order-101 -client worker-alpha -ttl 6 -interval 2

# Submit write to Protected Resource with Fencing Token
./lockforge-cli write -resource order-101 -client worker-alpha -token 101 -data '{"status":"PAID"}'

# Release a held lock
./lockforge-cli release -resource order-101 -client worker-alpha

# List all active locks across the cluster
./lockforge-cli list
```

---

## Docker Compose Deployment

To run a multi-node etcd Raft cluster, Redis cache, LockForge backend, and Nginx React frontend:

```bash
docker compose up --build
```
- **Web Dashboard**: `http://localhost:3000`
- **Backend API & Gateway**: `http://localhost:8080`
- **gRPC Service**: `localhost:50051`
- **etcd Cluster**: 3-node cluster on ports 2379, 22379, 32379
- **Redis Cache**: `localhost:6379`

---

## Project Structure

```
lockforge/
├── cmd/
│   ├── lockforge-server/       # Main gRPC + HTTP/SSE server entrypoint
│   ├── lockforge-cli/          # CLI tool (acquire, release, heartbeat, write, etc.)
│   ├── lockforge-benchmark/    # Performance & consistency benchmarking tool
│   └── protected-resource/     # Standalone protected resource server
├── internal/
│   ├── lockmanager/            # Core LockManager (CAS, ownership, watch events)
│   ├── etcdclient/             # etcd v3 client interface, real client & in-memory Raft
│   ├── fencing/                # Monotonic fencing token generator
│   ├── protectedresource/      # Storage guard validating fencing tokens
│   ├── redisadapter/           # Redis read-through cache with fallback
│   ├── grpcserver/             # gRPC service implementation
│   ├── dashboard/              # HTTP REST & SSE stream handlers
│   └── events/                 # Circular ring buffer & pub/sub broadcaster
├── proto/
│   ├── lockforge.proto         # Protocol buffer service definitions
│   ├── lockforge.pb.go         # Compiled protobuf structs
│   └── lockforge_grpc.pb.go    # Compiled gRPC client/server interfaces
├── web/                        # React 19 + TypeScript + Vite Dashboard
│   ├── src/                    # Components, hooks, types
│   └── dist/                   # Production build bundled with backend
├── tests/
│   ├── integration_test.go     # End-to-end integration & concurrency tests
│   ├── grpc_service_test.go    # In-memory gRPC tests via bufconn
│   ├── redis_cache_test.go     # Redis caching & TTL expiration tests
│   └── fencing_test.go         # Fencing token monotonicity & guard tests
├── docs/
│   ├── PROJECT_REPORT.md       # Mini-project report (Roll No. CS-9044)
│   └── DEMO_SCRIPT.md          # 5-minute presentation & viva script
├── scripts/
│   ├── demo.sh                 # Step-by-step demonstration runner
│   ├── chaos.sh                # Chaos & fault injection runner
│   └── benchmark.sh            # Benchmark runner
├── docker-compose.yml          # 3-node etcd, Redis, backend, frontend
├── Dockerfile                  # Multi-stage Go backend Dockerfile
└── README.md                   # Project documentation
```

---

## Documentation & Viva Preparation

- Comprehensive Project Report: [docs/PROJECT_REPORT.md](docs/PROJECT_REPORT.md)
- 5-Minute Presentation & Viva Script: [docs/DEMO_SCRIPT.md](docs/DEMO_SCRIPT.md)
- Online Cloud Deployment Guide: [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md)

---

## Deploying to GitHub & Online Cloud

### 1. Push to GitHub
```bash
# Push with GitHub CLI
gh auth login
gh repo create lockforge --public --source=. --remote=origin --push

# Or add your repository URL manually:
git remote add origin https://github.com/<your-username>/lockforge.git
git push -u origin main
```

### 2. Deploy Online (Free 1-Click Hosting)
- **Render**: Connect your GitHub repo to [Render](https://dashboard.render.com) (uses included `render.yaml`).
- **Railway**: Connect your GitHub repo to [Railway](https://railway.app) (uses included `railway.json`).
- **Fly.io**: Run `fly launch --now` (uses included `fly.toml`).
- **Full Guide**: See [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md).

