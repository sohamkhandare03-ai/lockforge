#!/usr/bin/env bash
set -e

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$DIR"

echo "=========================================================================="
echo "         LOCKFORGE – DISTRIBUTED LOCK MANAGER DEMONSTRATION               "
echo "=========================================================================="

# Build CLI if not present
if [ ! -f ./lockforge-cli ]; then
  echo "[1/6] Building lockforge-cli..."
  go build -o lockforge-cli ./cmd/lockforge-cli
fi

# Check if server is running, if not start background server
SERVER_PID=""
if ! curl -s http://localhost:8080/api/status > /dev/null; then
  echo "[1/6] Starting LockForge server in background..."
  go build -o lockforge-server ./cmd/lockforge-server
  ./lockforge-server --use-memory > /dev/null 2>&1 &
  SERVER_PID=$!
  sleep 1
fi

cleanup() {
  if [ -n "$SERVER_PID" ]; then
    echo "Stopping background server (PID $SERVER_PID)..."
    kill "$SERVER_PID" 2>/dev/null || true
  fi
}
trap cleanup EXIT

echo -e "\n[2/6] Step 1: Normal Lock Acquisition with Lease TTL & Fencing Token"
./lockforge-cli acquire -resource database-migration -client worker-alpha -ttl 10

echo -e "\n[3/6] Step 2: Inspect Lock Status and Monotonic Fencing Token"
./lockforge-cli inspect -resource database-migration

echo -e "\n[4/6] Step 3: Concurrent Contention (worker-beta tries to acquire same resource)"
./lockforge-cli acquire -resource database-migration -client worker-beta -ttl 10 || true

echo -e "\n[5/6] Step 4: Stale-Write Guard (Kleppmann Fencing Validation)"
echo "Worker Alpha submits write with fencing token 101:"
./lockforge-cli write -resource database-migration -client worker-alpha -token 101 -data "migration=v1.0"

echo -e "\nSimulating Worker Alpha crash and Worker Beta taking over with fencing token 102:"
./lockforge-cli write -resource database-migration -client worker-beta -token 102 -data "migration=v2.0"

echo -e "\nStale Worker Alpha wakes up after GC pause and attempts write with old token 101:"
./lockforge-cli write -resource database-migration -client worker-alpha -token 101 -data "migration=stale_v1" || true

echo -e "\n[6/6] Step 5: Safe Lock Release & Cluster Status"
./lockforge-cli release -resource database-migration -client worker-alpha
./lockforge-cli list

echo -e "\n=========================================================================="
echo "             DEMONSTRATION COMPLETED SUCCESSFULLY!                        "
echo "=========================================================================="
