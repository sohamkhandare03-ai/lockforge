#!/usr/bin/env bash
set -e

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$DIR"

echo "=========================================================================="
echo "          LOCKFORGE CHAOS & FAULT INJECTION TEST SUITE                    "
echo "=========================================================================="

echo -e "\n[Chaos 1/4] Running 15-Client Concurrent Acquisition Race..."
go test -v ./tests -run TestConcurrentAcquisition

echo -e "\n[Chaos 2/4] Running Client Crash & Auto-Recovery Test..."
go test -v ./tests -run TestLeaseExpiryAndRecovery

echo -e "\n[Chaos 3/4] Running Martin Kleppmann Stale-Write Rejection Test..."
go test -v ./tests -run TestStaleWriteRejection

echo -e "\n[Chaos 4/4] Running Network Partition & Quorum Loss Safety Test..."
go test -v ./tests -run TestNetworkPartitionQuorumSafety

echo -e "\n=========================================================================="
echo "          ALL 4 CHAOS SCENARIOS PASSED WITH ZERO VIOLATIONS!              "
echo "=========================================================================="
