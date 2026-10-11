package main

import (
	"context"
	"flag"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/lockforge/lockforge/internal/etcdclient"
	"github.com/lockforge/lockforge/internal/events"
	"github.com/lockforge/lockforge/internal/fencing"
	"github.com/lockforge/lockforge/internal/lockmanager"
	"github.com/lockforge/lockforge/internal/redisadapter"
)

type BenchStats struct {
	Engine     string
	TotalOps   int
	Duration   time.Duration
	OpsPerSec  float64
	AvgMs      float64
	MinMs      float64
	MaxMs      float64
	P50Ms      float64
	P95Ms      float64
	P99Ms      float64
	Successes  int
	Failures   int
}

func main() {
	concurrency := flag.Int("c", 10, "Concurrency level")
	totalOps := flag.Int("n", 200, "Total number of operations per engine")
	etcdEndpoints := flag.String("etcd", "", "etcd endpoints comma-separated (empty for in-memory Raft)")
	redisAddr := flag.String("redis", "", "Redis address (empty for in-memory)")
	flag.Parse()

	fmt.Println("==========================================================================")
	fmt.Println("         LOCKFORGE PERFORMANCE & CONSISTENCY BENCHMARK                    ")
	fmt.Printf("  Concurrency: %d workers | Operations: %d ops per engine\n", *concurrency, *totalOps)
	fmt.Println("==========================================================================")

	// 1. Setup etcd & LockManager
	var endpoints []string
	if *etcdEndpoints != "" {
		endpoints = strings.Split(*etcdEndpoints, ",")
	}
	client, _ := etcdclient.NewClient(etcdclient.Config{
		Endpoints:   endpoints,
		UseMemory:   len(endpoints) == 0,
		DialTimeout: 2 * time.Second,
	})
	defer client.Close()

	evBuffer := events.NewRingBuffer(100)
	fencingGen := fencing.NewGenerator(client)
	redisAdapter := redisadapter.NewAdapter(*redisAddr)
	lm := lockmanager.NewLockManager(client, fencingGen, redisAdapter, evBuffer, lockmanager.Config{
		DefaultTTL: 5,
	})
	defer lm.Close()

	// Run etcd benchmark
	fmt.Print("Benchmarking etcd (Linearizable Raft CAS + Leases)... ")
	etcdStats := runEtcdBenchmark(lm, *concurrency, *totalOps)
	fmt.Println("DONE.")

	// Run Redis benchmark
	fmt.Print("Benchmarking Redis (In-Memory / Read-Through Cache)... ")
	redisStats := runRedisBenchmark(redisAdapter, *concurrency, *totalOps)
	fmt.Println("DONE.")

	printReport(etcdStats, redisStats)
}

func runEtcdBenchmark(lm *lockmanager.LockManager, concurrency, totalOps int) BenchStats {
	opsPerWorker := totalOps / concurrency
	var wg sync.WaitGroup
	latencies := make([]float64, 0, totalOps)
	var latMu sync.Mutex
	successCount := 0
	failCount := 0

	startTime := time.Now()

	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		workerID := fmt.Sprintf("worker-%02d", w)
		go func(wID string) {
			defer wg.Done()
			ctx := context.Background()

			for i := 0; i < opsPerWorker; i++ {
				resID := fmt.Sprintf("res-%s-%d", wID, i)
				t0 := time.Now()
				resp, err := lm.AcquireLock(ctx, resID, wID, 5)
				if err == nil && resp != nil && resp.Acquired {
					_, _ = lm.ReleaseLock(ctx, resID, wID, resp.LeaseId, resp.FencingToken)
					elapsedMs := float64(time.Since(t0).Microseconds()) / 1000.0
					latMu.Lock()
					latencies = append(latencies, elapsedMs)
					successCount++
					latMu.Unlock()
				} else {
					latMu.Lock()
					failCount++
					latMu.Unlock()
				}
			}
		}(workerID)
	}

	wg.Wait()
	dur := time.Since(startTime)

	return computeStats("etcd (Linearizable Raft)", totalOps, dur, latencies, successCount, failCount)
}

func runRedisBenchmark(adapter *redisadapter.Adapter, concurrency, totalOps int) BenchStats {
	opsPerWorker := totalOps / concurrency
	var wg sync.WaitGroup
	latencies := make([]float64, 0, totalOps)
	var latMu sync.Mutex
	successCount := 0
	failCount := 0

	startTime := time.Now()

	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		workerID := fmt.Sprintf("worker-%02d", w)
		go func(wID string) {
			defer wg.Done()
			ctx := context.Background()

			for i := 0; i < opsPerWorker; i++ {
				resID := fmt.Sprintf("res-redis-%s-%d", wID, i)
				t0 := time.Now()
				err := adapter.CacheSet(ctx, resID, &redisadapter.CachedLock{
					ResourceID:   resID,
					OwnerID:      wID,
					FencingToken: int64(i + 1),
					ExpiresAtMs:  time.Now().Add(5 * time.Second).UnixMilli(),
				}, 5*time.Second)
				_, _ = adapter.CacheGet(ctx, resID)
				_ = adapter.CacheDelete(ctx, resID)

				elapsedMs := float64(time.Since(t0).Microseconds()) / 1000.0
				latMu.Lock()
				if err == nil {
					successCount++
					latencies = append(latencies, elapsedMs)
				} else {
					failCount++
				}
				latMu.Unlock()
			}
		}(workerID)
	}

	wg.Wait()
	dur := time.Since(startTime)

	return computeStats("Redis (Read Shortcut)", totalOps, dur, latencies, successCount, failCount)
}

func computeStats(name string, totalOps int, dur time.Duration, latencies []float64, successes, failures int) BenchStats {
	sort.Float64s(latencies)
	n := len(latencies)
	if n == 0 {
		return BenchStats{Engine: name, Duration: dur}
	}

	sum := 0.0
	for _, l := range latencies {
		sum += l
	}

	return BenchStats{
		Engine:     name,
		TotalOps:   totalOps,
		Duration:   dur,
		OpsPerSec:  float64(totalOps) / dur.Seconds(),
		AvgMs:      sum / float64(n),
		MinMs:      latencies[0],
		MaxMs:      latencies[n-1],
		P50Ms:      latencies[int(float64(n)*0.50)],
		P95Ms:      latencies[int(float64(n)*0.95)],
		P99Ms:      latencies[int(float64(n)*0.99)],
		Successes:  successes,
		Failures:   failures,
	}
}

func printReport(s1, s2 BenchStats) {
	fmt.Println("\n==========================================================================")
	fmt.Println("                       BENCHMARK RESULTS REPORT                           ")
	fmt.Println("==========================================================================")
	fmt.Printf("%-24s %-12s %-10s %-10s %-10s %-10s\n", "ENGINE", "OPS/SEC", "AVG (ms)", "P50 (ms)", "P95 (ms)", "P99 (ms)")
	fmt.Println(strings.Repeat("-", 80))
	fmt.Printf("%-24s %-12.1f %-10.2f %-10.2f %-10.2f %-10.2f\n", s1.Engine, s1.OpsPerSec, s1.AvgMs, s1.P50Ms, s1.P95Ms, s1.P99Ms)
	fmt.Printf("%-24s %-12.1f %-10.2f %-10.2f %-10.2f %-10.2f\n", s2.Engine, s2.OpsPerSec, s2.AvgMs, s2.P50Ms, s2.P95Ms, s2.P99Ms)
	fmt.Println("==========================================================================")
	fmt.Println("\nArchitectural Trade-Off Analysis:")
	fmt.Println("1. etcd (Raft): Strong CP consistency. Every acquire goes through consensus log")
	fmt.Println("   replication. Ensures safety across network partitions and prevents split-brain.")
	fmt.Println("2. Redis: In-memory AP cache. Offers sub-millisecond read shortcut latencies,")
	fmt.Println("   used in LockForge to offload frequent status checks without burdening Raft.")
}
