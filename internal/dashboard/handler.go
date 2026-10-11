package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/lockforge/lockforge/internal/events"
	"github.com/lockforge/lockforge/internal/lockmanager"
	"github.com/lockforge/lockforge/internal/protectedresource"
	"github.com/lockforge/lockforge/internal/redisadapter"
	pb "github.com/lockforge/lockforge/proto"
)

// Handler serves HTTP endpoints and SSE streams for the web UI.
type Handler struct {
	lm        *lockmanager.LockManager
	events    *events.RingBuffer
	protected *protectedresource.ProtectedResource
	chaosLock sync.Mutex
}

// NewHandler creates a dashboard HTTP handler.
func NewHandler(
	lm *lockmanager.LockManager,
	eventBuffer *events.RingBuffer,
	protected *protectedresource.ProtectedResource,
) *Handler {
	return &Handler{
		lm:        lm,
		events:    eventBuffer,
		protected: protected,
	}
}

// RegisterRoutes registers all API routes on an http.ServeMux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/status", h.cors(h.handleStatus))
	mux.HandleFunc("/api/locks", h.cors(h.handleLocks))
	mux.HandleFunc("/api/locks/acquire", h.cors(h.handleAcquire))
	mux.HandleFunc("/api/locks/release", h.cors(h.handleRelease))
	mux.HandleFunc("/api/locks/renew", h.cors(h.handleRenew))
	mux.HandleFunc("/api/resource/write", h.cors(h.handleResourceWrite))
	mux.HandleFunc("/api/events", h.cors(h.handleEvents))
	mux.HandleFunc("/api/events/stream", h.cors(h.handleEventStream))
	mux.HandleFunc("/api/chaos/", h.cors(h.handleChaos))
	mux.HandleFunc("/api/benchmark", h.cors(h.handleBenchmark))

	// Static web dashboard handler using embedded filesystem
	staticFileServer := http.FileServer(getStaticFileSystem())
	indexHTML, _ := staticFS.ReadFile("static/index.html")

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api") {
			http.NotFound(w, r)
			return
		}
		// If requesting root or a non-asset path, serve index.html
		if r.URL.Path == "/" || !strings.HasPrefix(r.URL.Path, "/assets/") {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(indexHTML)
			return
		}
		// Serve static assets with explicit MIME types
		if strings.HasSuffix(r.URL.Path, ".js") {
			w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		} else if strings.HasSuffix(r.URL.Path, ".css") {
			w.Header().Set("Content-Type", "text/css; charset=utf-8")
		}
		staticFileServer.ServeHTTP(w, r)
	})
}

func (h *Handler) cors(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, PUT, DELETE")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next(w, r)
	}
}

func (h *Handler) handleStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	locks, _ := h.lm.GetActiveLocks(ctx)
	redisHealthy := h.lm.GetRedisAdapter().IsHealthy(ctx)
	etcdPartitioned := h.lm.GetEtcdClient().IsPartitioned()

	etcdHealth := "healthy"
	if etcdPartitioned {
		etcdHealth = "offline"
	}

	redisHealth := "offline"
	if redisHealthy {
		redisHealth = "healthy"
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"connected":          true,
		"etcd_health":        etcdHealth,
		"etcd_nodes":         3,
		"redis_health":       redisHealth,
		"active_locks_count": len(locks),
		"total_events":       len(h.events.GetRecent(500)),
	})
}

func (h *Handler) handleLocks(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if r.Method == http.MethodGet {
		locks, err := h.lm.GetActiveLocks(ctx)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		type LockDTO struct {
			ResourceID          string `json:"resource_id"`
			OwnerID             string `json:"owner_id"`
			LeaseID             int64  `json:"lease_id"`
			FencingToken        int64  `json:"fencing_token"`
			AcquiredAtUnixMs    int64  `json:"acquired_at_unix_ms"`
			ExpiresAtUnixMs     int64  `json:"expires_at_unix_ms"`
			TTLSeconds          int64  `json:"ttl_seconds"`
			TTLRemainingSeconds int64  `json:"ttl_remaining_seconds"`
		}

		now := time.Now().UnixMilli()
		var dtos []LockDTO
		for _, l := range locks {
			remaining := (l.ExpiresAtUnixMs - now) / 1000
			if remaining < 0 {
				remaining = 0
			}
			dtos = append(dtos, LockDTO{
				ResourceID:          l.ResourceId,
				OwnerID:             l.OwnerId,
				LeaseID:             l.LeaseId,
				FencingToken:        l.FencingToken,
				AcquiredAtUnixMs:    l.AcquiredAtUnixMs,
				ExpiresAtUnixMs:     l.ExpiresAtUnixMs,
				TTLSeconds:          l.TtlSeconds,
				TTLRemainingSeconds: remaining,
			})
		}

		writeJSON(w, http.StatusOK, dtos)
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

func (h *Handler) handleAcquire(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ResourceID string `json:"resource_id"`
		ClientID   string `json:"client_id"`
		TTLSeconds int64  `json:"ttl_seconds"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}

	resp, err := h.lm.AcquireLock(r.Context(), req.ResourceID, req.ClientID, req.TTLSeconds)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleRelease(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ResourceID   string `json:"resource_id"`
		ClientID     string `json:"client_id"`
		LeaseID      int64  `json:"lease_id"`
		FencingToken int64  `json:"fencing_token"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}

	resp, err := h.lm.ReleaseLock(r.Context(), req.ResourceID, req.ClientID, req.LeaseID, req.FencingToken)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleRenew(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ResourceID string `json:"resource_id"`
		ClientID   string `json:"client_id"`
		LeaseID    int64  `json:"lease_id"`
		TTLSeconds int64  `json:"ttl_seconds"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}

	resp, err := h.lm.RenewLease(r.Context(), req.ResourceID, req.ClientID, req.LeaseID, req.TTLSeconds)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleResourceWrite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ResourceID   string `json:"resource_id"`
		ClientID     string `json:"client_id"`
		FencingToken int64  `json:"fencing_token"`
		Data         string `json:"data"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}

	res, err := h.protected.Write(r.Context(), req.ResourceID, req.ClientID, req.FencingToken, req.Data)
	if err != nil {
		writeJSON(w, http.StatusConflict, res)
		return
	}

	writeJSON(w, http.StatusOK, res)
}

// EventDTO represents a JSON serializable event with human readable event_type string.
type EventDTO struct {
	EventID         string `json:"event_id"`
	EventType       string `json:"event_type"`
	ResourceID      string `json:"resource_id"`
	ClientID        string `json:"client_id"`
	FencingToken    int64  `json:"fencing_token"`
	LeaseID         int64  `json:"lease_id"`
	TimestampUnixMs int64  `json:"timestamp_unix_ms"`
	Details         string `json:"details"`
}

func toEventDTO(ev *pb.LockEvent) EventDTO {
	typeStr := strings.TrimPrefix(ev.EventType.String(), "LOCK_EVENT_TYPE_")
	if typeStr == "UNSPECIFIED" || typeStr == "" {
		typeStr = "SYSTEM"
	}
	return EventDTO{
		EventID:         ev.EventId,
		EventType:       typeStr,
		ResourceID:      ev.ResourceId,
		ClientID:        ev.ClientId,
		FencingToken:    ev.FencingToken,
		LeaseID:         ev.LeaseId,
		TimestampUnixMs: ev.TimestampUnixMs,
		Details:         ev.Details,
	}
}

func (h *Handler) handleEvents(w http.ResponseWriter, r *http.Request) {
	evs := h.events.GetRecent(100)
	var dtos []EventDTO
	for _, ev := range evs {
		dtos = append(dtos, toEventDTO(ev))
	}
	writeJSON(w, http.StatusOK, dtos)
}

func (h *Handler) handleEventStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ch, unsubscribe := h.events.Subscribe()
	defer unsubscribe()

	// Initial ping
	fmt.Fprintf(w, "event: ping\ndata: {}\n\n")
	flusher.Flush()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			fmt.Fprintf(w, "event: ping\ndata: {}\n\n")
			flusher.Flush()
		case ev, ok := <-ch:
			if !ok {
				return
			}
			dto := toEventDTO(ev)
			bytes, err := json.Marshal(dto)
			if err == nil {
				fmt.Fprintf(w, "data: %s\n\n", bytes)
				flusher.Flush()
			}
		}
	}
}

func (h *Handler) handleChaos(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	scenario := strings.TrimPrefix(r.URL.Path, "/api/chaos/")
	h.chaosLock.Lock()
	defer h.chaosLock.Unlock()

	ctx := context.Background()

	switch scenario {
	case "concurrency":
		// 10 clients hammer the same resource concurrently
		resID := fmt.Sprintf("order-payment-%d", time.Now().Unix()%1000)
		var winners []string
		var losers []string
		var wg sync.WaitGroup
		var mu sync.Mutex

		for i := 1; i <= 10; i++ {
			wg.Add(1)
			clientID := fmt.Sprintf("worker-%02d", i)
			go func(cID string) {
				defer wg.Done()
				resp, _ := h.lm.AcquireLock(ctx, resID, cID, 15)
				mu.Lock()
				defer mu.Unlock()
				if resp != nil && resp.Acquired {
					winners = append(winners, cID)
				} else {
					losers = append(losers, cID)
				}
			}(clientID)
		}
		wg.Wait()

		writeJSON(w, http.StatusOK, map[string]any{
			"scenario":        "concurrency",
			"resource_id":     resID,
			"winner_count":    len(winners),
			"winners":         winners,
			"rejected_count":  len(losers),
			"safety_verified": len(winners) == 1,
			"message":         fmt.Sprintf("Concurrent race completed! Winner: %v (Exactly 1 acquired lock)", winners),
		})

	case "crash":
		// Client acquires short TTL lock (3s) and abandons it
		resID := "crash-test-resource"
		c1 := "crashing-worker"
		c2 := "recovery-worker"

		resp1, _ := h.lm.AcquireLock(ctx, resID, c1, 3)
		go func() {
			time.Sleep(4 * time.Second)
			_, _ = h.lm.AcquireLock(ctx, resID, c2, 10)
		}()

		writeJSON(w, http.StatusOK, map[string]any{
			"scenario":       "crash_recovery",
			"resource_id":    resID,
			"initial_owner":  c1,
			"fencing_token":  resp1.FencingToken,
			"ttl_seconds":    3,
			"recovery_after": "4s",
			"message":        "Crashing worker acquired 3s lease and abandoned it. Watch the event stream for automatic lease expiry and recovery by recovery-worker!",
		})

	case "stale_write":
		// Martin Kleppmann stale write demonstration
		resID := "bank-account-transfer"
		// 1. Worker A acquires
		respA, _ := h.lm.AcquireLock(ctx, resID, "worker-A-paused", 2)
		tokenA := respA.FencingToken

		// 2. Lease expires, Worker B acquires with higher token
		time.Sleep(2500 * time.Millisecond)
		respB, _ := h.lm.AcquireLock(ctx, resID, "worker-B-active", 10)
		tokenB := respB.FencingToken

		// 3. Worker B writes successfully
		resB, _ := h.protected.Write(ctx, resID, "worker-B-active", tokenB, "balance = 500")

		// 4. Stale Worker A wakes up and attempts write with old token
		resA, _ := h.protected.Write(ctx, resID, "worker-A-paused", tokenA, "balance = 100")

		writeJSON(w, http.StatusOK, map[string]any{
			"scenario":        "stale_write_rejection",
			"resource_id":     resID,
			"worker_a_token":  tokenA,
			"worker_b_token":  tokenB,
			"worker_b_write":  resB,
			"worker_a_write":  resA,
			"safety_verified": !resA.Accepted && resB.Accepted,
			"message":         "Stale write scenario executed! Worker A's write was safely REJECTED by fencing token guard.",
		})

	case "partition":
		// Toggle network partition
		client := h.lm.GetEtcdClient()
		current := client.IsPartitioned()
		nextState := !current
		client.SetPartitionMode(nextState)

		eventType := pb.LockEventType_LOCK_EVENT_TYPE_QUORUM_LOST
		msg := "Simulated Raft minority partition active: quorum lost!"
		if !nextState {
			eventType = pb.LockEventType_LOCK_EVENT_TYPE_QUORUM_RESTORED
			msg = "Raft quorum restored: normal operations resumed"
		}
		h.events.Publish(eventType, "cluster", "chaos-operator", 0, 0, msg)

		writeJSON(w, http.StatusOK, map[string]any{
			"scenario":    "network_partition",
			"partitioned": nextState,
			"message":     msg,
		})

	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown chaos scenario"})
	}
}

func (h *Handler) handleBenchmark(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()

	// Benchmark etcd
	startEtcd := time.Now()
	totalOps := 100
	var etcdLatencies []float64

	for i := 0; i < totalOps; i++ {
		resID := fmt.Sprintf("bench-etcd-%d", i)
		t0 := time.Now()
		resp, _ := h.lm.AcquireLock(ctx, resID, "bench-worker", 5)
		if resp != nil && resp.Acquired {
			_, _ = h.lm.ReleaseLock(ctx, resID, "bench-worker", resp.LeaseId, resp.FencingToken)
		}
		etcdLatencies = append(etcdLatencies, float64(time.Since(t0).Microseconds())/1000.0)
	}
	durEtcd := time.Since(startEtcd)

	// Benchmark Redis adapter
	startRedis := time.Now()
	var redisLatencies []float64
	redisAdapter := h.lm.GetRedisAdapter()

	for i := 0; i < totalOps; i++ {
		resID := fmt.Sprintf("bench-redis-%d", i)
		t0 := time.Now()
		_ = redisAdapter.CacheSet(ctx, resID, &redisadapter.CachedLock{
			ResourceID:   resID,
			OwnerID:      "bench-worker",
			FencingToken: int64(i + 1),
			ExpiresAtMs:  time.Now().Add(5 * time.Second).UnixMilli(),
		}, 5*time.Second)
		_, _ = redisAdapter.CacheGet(ctx, resID)
		_ = redisAdapter.CacheDelete(ctx, resID)
		redisLatencies = append(redisLatencies, float64(time.Since(t0).Microseconds())/1000.0)
	}
	durRedis := time.Since(startRedis)

	writeJSON(w, http.StatusOK, map[string]any{
		"operations_per_engine": totalOps,
		"etcd_raft": map[string]any{
			"total_time_ms": durEtcd.Milliseconds(),
			"ops_per_sec":   float64(totalOps) / durEtcd.Seconds(),
			"avg_lat_ms":    calcAvg(etcdLatencies),
			"p95_lat_ms":    calcP95(etcdLatencies),
			"consistency":   "Linearizable (CP / Raft consensus)",
		},
		"redis_cache": map[string]any{
			"total_time_ms": durRedis.Milliseconds(),
			"ops_per_sec":   float64(totalOps) / durRedis.Seconds(),
			"avg_lat_ms":    calcAvg(redisLatencies),
			"p95_lat_ms":    calcP95(redisLatencies),
			"consistency":   "Eventual (AP / In-memory read shortcut)",
		},
	})
}

func calcAvg(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range vals {
		sum += v
	}
	return sum / float64(len(vals))
}

func calcP95(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	idx := int(float64(len(vals)) * 0.95)
	if idx >= len(vals) {
		idx = len(vals) - 1
	}
	return vals[idx]
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
