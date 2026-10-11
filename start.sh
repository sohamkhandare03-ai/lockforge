#!/usr/bin/env bash
set -e

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$DIR"

echo "=========================================================================="
echo "          LOCKFORGE – DISTRIBUTED LOCK MANAGER STARTER                    "
echo "=========================================================================="

echo "[1/2] Compiling LockForge binaries..."
go build -o lockforge-server ./cmd/lockforge-server
go build -o lockforge-cli ./cmd/lockforge-cli

echo -e "\n[2/2] Starting LockForge Server..."
echo "  • HTTP Web Dashboard: http://localhost:8080"
echo "  • gRPC Service:       localhost:50051"
echo "=========================================================================="
echo "Open your browser to: http://localhost:8080"
echo "Press Ctrl+C to stop the server."
echo "=========================================================================="

# Try to automatically open browser on macOS
if command -v open > /dev/null 2>&1; then
  (sleep 1 && open "http://localhost:8080") &
fi

exec ./lockforge-server --use-memory
