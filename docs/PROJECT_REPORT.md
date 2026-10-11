# Mini-Project Report: LockForge – Distributed Lock Manager

**Course:** T.Y.B.Sc. Computer Science Mini Project  
**Project Title:** LockForge – Distributed Lock Manager with Strong Consensus, Lease Management, and Fencing Tokens  
**Roll No:** CS-9044  
**Date:** September 2026  

---

## 1. Abstract

Distributed systems frequently require mutual exclusion when coordinating access to shared state (such as financial transactions, inventory adjustments, and database schema migrations). However, naive distributed locks built upon basic cache stores (e.g., standard Redis `SETNX`) fail during process pauses (such as garbage collection stalls or thread scheduler starvation) and network partitions, causing catastrophic "split-brain" dual-writer corruption.

**LockForge** is a distributed lock manager engineered to guarantee safety and correctness under real-world failure modes. Built in Go with gRPC, etcd Raft consensus, and Redis read-through caching, LockForge implements:
1. **Linearizable Mutual Exclusion** backed by etcd Compare-And-Swap (CAS) transactions.
2. **Dynamic Lease Management** with heartbeat keep-alive loops and automatic TTL reclamation upon client crashes.
3. **Monotonic Fencing Tokens** based on the Martin Kleppmann theorem to guard external protected storage engines against stale writes from delayed clients.
4. **Interactive Real-Time Observability** via a single-page React + TypeScript dashboard backed by Server-Sent Events (SSE) and gRPC streaming.

---

## 2. Problem Statement & Theoretical Motivation

### 2.1 The Split-Brain Vulnerability in Asynchronous Networks

In an asynchronous network, client processes cannot distinguish between network latency, process pauses (e.g., full Stop-The-World garbage collection or swapping), and machine crashes. 

Consider the classic Martin Kleppmann scenario:
1. **Client 1** acquires a lock on a shared resource with a 10-second TTL.
2. Client 1 begins an I/O operation but enters an unforeseen 15-second GC pause.
3. The lock manager's lease timer expires after 10 seconds and automatically releases the lock.
4. **Client 2** acquires the lock on the same resource and updates the shared storage.
5. **Client 1** wakes up from its GC pause, incorrectly believing it still holds the lock, and issues its write to shared storage, overwriting and corrupting Client 2's data.

```
Client 1:  [Acquire Lock] -----(STW GC Pause)-------------------> [Stale Write to Storage] (DATA CORRUPTED!)
                                  |
Lock Manager:                     [TTL Expired]
                                        |
Client 2:                               [Acquire Lock] ---> [Valid Write to Storage]
```

### 2.2 The Solution: Monotonic Fencing Tokens

To eliminate this vulnerability, the distributed lock manager must issue a **monotonically increasing fencing token** (a strictly increasing 64-bit integer) with every lock grant. The target storage system or protected resource acts as a gatekeeper: it records the highest token it has ever observed and unconditionally **rejects** any write request accompanied by a lower or equal token.

When Client 1 wakes up with Token #101, Client 2 has already committed with Token #102. The storage engine compares $101 < 102$ and rejects Client 1's write, preserving data integrity.

---

## 3. System Architecture & Components

```
+-----------------------------------------------------------------------------------+
|                        LockForge Unified Architecture                             |
+-----------------------------------------------------------------------------------+

     +-----------------------+                    +---------------------------+
     |  React + TypeScript   |                    |  gRPC Clients & Workers   |
     |   Web Dashboard       |                    |  (lockforge-cli, Services)|
     +-----------+-----------+                    +-------------+-------------+
                 | HTTP REST / SSE                              | gRPC (Protobuf)
                 | Port: 8080                                   | Port: 50051
                 v                                              v
     +------------------------------------------------------------------------+
     |                           LockForge Engine                             |
     |                                                                        |
     |  - LockManager: atomic CAS transactions, ownership checks              |
     |  - LeaseManager: etcd LeaseGrant, KeepAlive, and auto-revocation       |
     |  - FencingTokenGenerator: strictly monotonic persistent 64-bit counter |
     |  - Event RingBuffer: audit log & streaming broadcaster                 |
     |  - Redis Cache Adapter: fast read shortcut for InspectLock             |
     +-------------------+--------------------------------+-------------------+
                         |                                |
             Read-Cache  v                                v  Authoritative Raft
       +-----------------------+                    +---------------------------+
       |      Redis Cache      |                    |     etcd Consensus        |
       |  (Sub-ms Status Read) |                    |  (Linearizable CAS & TTL) |
       +-----------------------+                    +---------------------------+
                                                                  |
                                                                  v
                                                    +---------------------------+
                                                    |  Mock Protected Resource  |
                                                    |  (Kleppmann Token Guard)  |
                                                    +---------------------------+
```

### 3.1 LockManager & Compare-And-Swap (CAS)
LockForge wraps etcd v3 MVCC transactions (`clientv3.Txn`). During `AcquireLock`:
- The manager checks that `CreateRevision("/lockforge/locks/<resource_id>") == 0`.
- If true, it atomically writes a JSON payload containing the client ID, lease ID, fencing token, and timestamp.
- If false, the transaction fails atomically without partial state changes.

### 3.2 Lease Management & Heartbeat Loop
- Locks are associated with an etcd lease.
- Active clients maintain an asynchronous heartbeat loop invoking `RenewLease` (or `KeepAlive`).
- If a client terminates or network connectivity is lost, etcd expires the lease upon TTL timeout and triggers an etcd Watch deletion event, which updates the event stream.

### 3.3 Fencing Token Generator
- Fencing tokens are issued via an atomic counter stored at `/lockforge/counters/<resource_id>`.
- The sequence guarantees $Token_{t+1} > Token_t$ across all restarts and failovers.

### 3.4 Protected Resource Simulation
- Implements a guarded state machine.
- Maintains `last_seen_token`.
- Enforces the condition:
  $$\text{write\_allowed} \iff \text{token}_{\text{incoming}} > \text{token}_{\text{last\_seen}}$$
- Rejects violations with `ErrStaleWriteRejected` (HTTP 409 Conflict).

### 3.5 Redis Read-Through Cache
- Provides a fast read path for high-frequency `InspectLock` queries.
- Cached entries carry TTLs matching the underlying lease.
- Explicit releases or Watch delete events immediately invalidate the cache.

---

## 4. Verification & Experimental Results

The test suite exercises all critical failure scenarios:

| Test Case | Description | Verification Criterion | Result |
| :--- | :--- | :--- | :--- |
| `TestConcurrentAcquisition` | 15 goroutines concurrently race for the same resource | Exactly 1 client acquires; 14 receive `LockAlreadyHeld` | **PASS (0.00s)** |
| `TestLeaseExpiryAndRecovery` | Client acquires with 1s TTL and ceases heartbeats | Lock auto-releases after 1.2s; new client acquires with higher token | **PASS (1.20s)** |
| `TestStaleWriteRejection` | Client A acquires (token 101), GC pause; Client B acquires (token 102) & writes; Client A wakes and writes | Storage engine accepts Client B write; rejects Client A stale write | **PASS (1.20s)** |
| `TestNetworkPartitionQuorumSafety`| Injected network partition drops Raft below quorum | All acquire requests blocked/rejected; split-brain avoided | **PASS (0.00s)** |
| `TestRenewLeaseHeartbeat` | Client renews lease before TTL expires | Lock remains continuously held past original TTL | **PASS (2.50s)** |
| `TestRedisAdapterMemoryFallback`| Redis cache set, get, invalidate, hit/miss tracking | Cache hits and invalidations operate correctly | **PASS (0.00s)** |
| `TestRedisAdapterTTLAutoExpiry` | Cached lock expires past TTL | Cache misses after TTL expiration | **PASS (0.30s)** |
| `TestGRPCAcquireAndRelease` | Full gRPC proto Acquire, Inspect, Release sequence | All gRPC status codes and responses verified | **PASS (0.01s)** |
| `TestGRPCRenewLease` | gRPC RenewLease RPC keep-alive | Lease extended over gRPC | **PASS (0.00s)** |
| `TestGRPCGetActiveLocksAndStream`| gRPC ActiveLocks query & StreamUpdates event stream | Events streamed in real time | **PASS (0.00s)** |

All 13 unit and integration tests execute cleanly with **zero data races** detected by `go test -race`.

---

## 5. What Was Built vs. What Was Cut (Crunch-Mode Strategy)

In accordance with the crunch-mode project strategy:

### Completed Objectives:
- [x] Full gRPC service implementation (`AcquireLock`, `ReleaseLock`, `RenewLease`, `InspectLock`, `GetActiveLocks`, `StreamUpdates`).
- [x] etcd CompareAndSwap atomic lock acquisition and safe release.
- [x] etcd lease grant, TTL automatic expiry, and client heartbeat loop (`lockforge-cli heartbeat`).
- [x] Persistent monotonic fencing token generator.
- [x] Mock Protected Resource validating fencing tokens and rejecting stale writes.
- [x] Concurrency race testing (15 racing clients).
- [x] Stale client simulation and partition safety verification.
- [x] Redis caching layer for fast `InspectLock` reads with memory fallback.
- [x] Single-page React + TypeScript dashboard with live TTL countdowns, lock table, fencing timeline, chaos testing, and protected resource testing.
- [x] Complete Dockerfile and `docker-compose.yml` for multi-container deployment.
- [x] CLI control tool (`lockforge-cli`) with acquire, release, renew, heartbeat, inspect, list, write, chaos, and benchmark commands.

### Explicitly Cut for Time (Marked as Future Work):
- **Redlock Benchmarking Suite**: Excluded in favor of direct Raft CAS consensus. Redlock requires multiple uncoordinated Redis nodes and suffers from clock drift hazards (as analyzed by Kleppmann).
- **Physical Multi-Node Hardware Cluster**: Demonstrated using single-node etcd and in-memory Raft partition simulation; multi-node etcd deployment configured in `docker-compose.yml`.
- **Read-Write Locks & Counting Semaphores**: Kept out of scope to focus on mutually exclusive write locks.
- **Dashboard Authentication**: Omitted to prioritize core distributed consensus mechanics.

---

## 6. Conclusion

LockForge demonstrates how combining strongly consistent consensus logs (etcd Raft CAS) with client-side lease heartbeats and monotonic fencing tokens provides provable safety against process pauses, network partitions, and stale writes in distributed computing environments.

---

**Prepared by:** Student Roll No. CS-9044  
**Department:** Computer Science  
