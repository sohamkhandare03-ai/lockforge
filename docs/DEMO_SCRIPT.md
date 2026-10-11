# LockForge: 5-Minute Presentation & Viva Demo Script

**Project:** LockForge – Distributed Lock Manager (Roll No. CS-9044)  
**Duration:** 5 Minutes  
**Prerequisites:** Terminal open in project directory, browser open to `http://localhost:8080` (or `http://localhost:3000`).

---

## Demo Agenda & Timeline

| Time | Phase | Command / Action | Key Point to Explain to Examiner |
| :--- | :--- | :--- | :--- |
| **0:00 – 1:00** | **Architecture Overview** | Show architecture diagram in README or dashboard | Explain why simple Redis `SETNX` fails during GC pauses and how Raft CAS + Fencing Tokens solve it. |
| **1:00 – 2:00** | **Normal Acquire & Inspect** | `./lockforge-cli acquire -resource order-101 -client worker-alpha -ttl 15` | Show lock acquisition, lease ID, and issued **Fencing Token #101**. Show it appearing live in the dashboard table. |
| **2:00 – 3:00** | **Crash & Lease Expiry** | `./lockforge-cli heartbeat -resource order-101 -client worker-alpha -ttl 6 -interval 2` then press `Ctrl+C` | Demonstrate the client heartbeat renewing the lease. Kill client with `Ctrl+C`. Observe TTL bar running down in dashboard and auto-releasing! |
| **3:00 – 4:15** | **The Martin Kleppmann Fencing Token Demo** | Run Stale Write Scenario (CLI or Dashboard) | **The Core Viva Scenario:** Worker Alpha gets Token 101, pauses. Worker Beta gets Token 102 and writes. Alpha wakes up with Token 101 and tries to write -> **REJECTED (HTTP 409 Conflict)!** |
| **4:15 – 5:00** | **Concurrency Race & Summary** | `bash scripts/chaos.sh` or click Concurrency in UI | 15 goroutines race concurrently for 1 resource; exactly 1 winner, 14 safe rejections. Run automated test suite. |

---

## Detailed Step-by-Step Script

### Step 1: Start Backend & Dashboard (0:00)
In Terminal 1:
```bash
# Start LockForge server with in-memory Raft engine & static web dashboard
./lockforge-server --use-memory
```
*Open browser to `http://localhost:8080`*.  
Point out:
- Connection Status: "Healthy", Cluster nodes, Active Locks counter.
- Active Locks table (empty right now).
- Fencing Token Monotonic Progression Timeline.

---

### Step 2: Normal Lock Acquisition & Ownership Safety (1:00)
In Terminal 2:
```bash
./lockforge-cli acquire -resource database-migration -client worker-alpha -ttl 20
```
**Observation:**
- CLI returns: `Acquired: true`, `Owner: worker-alpha`, `Fencing Token: 101`, `TTL: 20s`.
- On Dashboard: `database-migration` appears with a live countdown bar, Token #101, and owner `worker-alpha`.

Try acquiring with a second worker:
```bash
./lockforge-cli acquire -resource database-migration -client worker-beta -ttl 20
```
**Observation:**
- Rejected with: `ErrorMessage: lock already held by client 'worker-alpha'`. Mutual exclusion verified!

---

### Step 3: Heartbeat Loop & Crash Simulation (2:00)
In Terminal 2:
```bash
./lockforge-cli heartbeat -resource invoice-gen -client billing-worker -ttl 6 -interval 2
```
**Observation:**
- CLI starts periodic heartbeats every 2 seconds: `Heartbeat ping sent: lease renewed (+6s)`.
- Dashboard shows the TTL bar being regularly refreshed.

**Simulate Process Crash:**
- Press `Ctrl+C` in Terminal 2.
- Explain: *"The billing worker has crashed or experienced a fatal network disconnect. It stopped sending heartbeats."*
- Watch the dashboard: in 6 seconds, the lease timer reaches 0, etcd automatically revokes the lease, and the lock disappears from active locks.

---

### Step 4: The Martin Kleppmann Stale-Write Defense (3:00) — *Strongest Viva Moment*

In the Dashboard "Chaos & Fault Injection Studio", click **"Run Martin Kleppmann Stale-Write Test"**, or run in Terminal:

```bash
# 1. Worker A acquires with short TTL and pauses
./lockforge-cli acquire -resource bank-ledger -client worker-A -ttl 2
# Fencing Token = 102

# 2. Lease expires, Worker B takes over
sleep 3
./lockforge-cli acquire -resource bank-ledger -client worker-B -ttl 10
# Fencing Token = 103

# 3. Worker B commits valid write to Protected Resource
./lockforge-cli write -resource bank-ledger -client worker-B -token 103 -data "balance=$500"
# OUTPUT: ACCEPTED! Storage last_seen_token is now 103.

# 4. Worker A wakes up from pause and attempts write with old Token 102:
./lockforge-cli write -resource bank-ledger -client worker-A -token 102 -data "balance=$100"
# OUTPUT: REJECTED! Error: stale write rejected: fencing token violation (102 <= 103)
```

**Explain to Examiner:**
> *"Even though Worker A believed it held the lock when it began processing, its token is stale. The protected storage engine rejected its write, completely preventing data corruption and split-brain."*

---

### Step 5: Concurrency Race & Test Suite (4:15)
Run the automated chaos test suite:
```bash
bash scripts/chaos.sh
```
**Observation:**
- 15 goroutines racing concurrently -> exactly 1 winner.
- Crash and auto-recovery verified.
- Stale write rejection verified.
- Partition quorum loss safety verified.
- All 4 chaos tests pass in ~3 seconds.

---

## Top 5 Likely Viva Questions & Answers

#### Q1: Why not just use Redis `SET resource_name my_random_value NX PX 30000`?
**Answer:** Redis `SETNX` provides mutual exclusion only assuming perfect timing. In asynchronous systems, if a process encounters a GC pause, page fault, or network delay longer than the TTL, the lock expires. Another process acquires it, and when the paused process resumes, both processes access the critical section concurrently. Furthermore, Redis `SETNX` does not issue monotonic fencing tokens to protect downstream databases.

#### Q2: How do your Fencing Tokens differ from random UUIDs?
**Answer:** Fencing tokens must be strictly monotonic (every new token $N+1 > N$). Downstream storage engines check if `incoming_token > last_seen_token`. A random UUID cannot be ordered, so the storage engine cannot tell which client arrived earlier or later.

#### Q3: What happens if etcd loses Raft quorum?
**Answer:** In a Raft cluster, writes require agreement from a majority $(N/2 + 1)$ of nodes. If network partition separates the cluster, the minority partition cannot commit transactions or grant new leases. This adheres to the CP (Consistency / Partition tolerance) guarantee of the CAP theorem, preventing split-brain.

#### Q4: Why is Redis used in your architecture if etcd is the consensus store?
**Answer:** etcd provides linearizable consistency, but reading from Raft consensus incurs disk/network overhead. Redis acts strictly as an in-memory **read-through cache** for frequent `InspectLock` status queries (e.g., UI polling every second), while etcd remains the sole authoritative source of truth for all lock mutations.

#### Q5: How is a lock safely released? Can a rogue client release someone else's lock?
**Answer:** No. LockForge verifies the client ID in the lock payload before allowing a delete. If client $B$ attempts to release client $A$'s lock, LockForge returns a permission denied error. Only the genuine owner can explicitly release the lock.
