package tests

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lockforge/lockforge/internal/dashboard"
	"github.com/lockforge/lockforge/internal/etcdclient"
	"github.com/lockforge/lockforge/internal/events"
	"github.com/lockforge/lockforge/internal/fencing"
	"github.com/lockforge/lockforge/internal/lockmanager"
	"github.com/lockforge/lockforge/internal/protectedresource"
	"github.com/lockforge/lockforge/internal/redisadapter"
)

func setupTestServer(t *testing.T) http.Handler {
	client := etcdclient.NewMemoryClient()
	fencingGen := fencing.NewGenerator(client)
	redisAdapter := redisadapter.NewAdapter("")
	eventBuffer := events.NewRingBuffer(500)
	protectedRes := protectedresource.NewProtectedResource(eventBuffer)

	lm := lockmanager.NewLockManager(client, fencingGen, redisAdapter, eventBuffer, lockmanager.Config{
		DefaultTTL: 10,
	})

	t.Cleanup(func() {
		lm.Close()
		client.Close()
	})

	handler := dashboard.NewHandler(lm, eventBuffer, protectedRes)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	return mux
}

func TestWebDashboardServing(t *testing.T) {
	mux := setupTestServer(t)

	// 1. Root / must return 200 and index.html
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for /, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "LockForge") || !strings.Contains(body, "root") {
		t.Fatalf("Expected HTML containing 'LockForge' and 'root', got: %s", body)
	}

	// 2. /api/status returns JSON status
	reqStatus := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	recStatus := httptest.NewRecorder()
	mux.ServeHTTP(recStatus, reqStatus)

	if recStatus.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for /api/status, got %d", recStatus.Code)
	}
	var statusMap map[string]any
	if err := json.Unmarshal(recStatus.Body.Bytes(), &statusMap); err != nil {
		t.Fatalf("Invalid JSON from /api/status: %v", err)
	}
	if statusMap["connected"] != true {
		t.Fatalf("Expected connected=true, got %v", statusMap["connected"])
	}

	// 3. Acquire Lock via REST API
	acqBody, _ := json.Marshal(map[string]any{
		"resource_id": "dashboard-resource",
		"client_id":   "dashboard-user",
		"ttl_seconds": 15,
	})
	reqAcq := httptest.NewRequest(http.MethodPost, "/api/locks/acquire", bytes.NewReader(acqBody))
	reqAcq.Header.Set("Content-Type", "application/json")
	recAcq := httptest.NewRecorder()
	mux.ServeHTTP(recAcq, reqAcq)

	if recAcq.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for /api/locks/acquire, got %d", recAcq.Code)
	}
	var acqResp map[string]any
	if err := json.Unmarshal(recAcq.Body.Bytes(), &acqResp); err != nil {
		t.Fatalf("Invalid JSON from acquire: %v", err)
	}
	if acqResp["acquired"] != true {
		t.Fatalf("Expected acquired=true, got %v", acqResp)
	}
	fencingToken := int64(acqResp["fencing_token"].(float64))

	// 4. GET /api/locks returns active lock
	reqLocks := httptest.NewRequest(http.MethodGet, "/api/locks", nil)
	recLocks := httptest.NewRecorder()
	mux.ServeHTTP(recLocks, reqLocks)

	if recLocks.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for /api/locks, got %d", recLocks.Code)
	}
	var activeLocks []map[string]any
	if err := json.Unmarshal(recLocks.Body.Bytes(), &activeLocks); err != nil {
		t.Fatalf("Invalid JSON from /api/locks: %v", err)
	}
	if len(activeLocks) != 1 || activeLocks[0]["resource_id"] != "dashboard-resource" {
		t.Fatalf("Active locks mismatch: %+v", activeLocks)
	}

	// 5. Test Protected Resource Write
	writeBody, _ := json.Marshal(map[string]any{
		"resource_id":   "dashboard-resource",
		"client_id":     "dashboard-user",
		"fencing_token": fencingToken,
		"data":          "inventory=100",
	})
	reqWrite := httptest.NewRequest(http.MethodPost, "/api/resource/write", bytes.NewReader(writeBody))
	reqWrite.Header.Set("Content-Type", "application/json")
	recWrite := httptest.NewRecorder()
	mux.ServeHTTP(recWrite, reqWrite)

	if recWrite.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for /api/resource/write, got %d", recWrite.Code)
	}
	var writeResp map[string]any
	_ = json.Unmarshal(recWrite.Body.Bytes(), &writeResp)
	if writeResp["accepted"] != true {
		t.Fatalf("Expected accepted=true, got %v", writeResp)
	}

	// 6. Test Stale Write Rejection (token = fencingToken - 1)
	staleBody, _ := json.Marshal(map[string]any{
		"resource_id":   "dashboard-resource",
		"client_id":     "stale-user",
		"fencing_token": fencingToken - 1,
		"data":          "inventory=999",
	})
	reqStale := httptest.NewRequest(http.MethodPost, "/api/resource/write", bytes.NewReader(staleBody))
	reqStale.Header.Set("Content-Type", "application/json")
	recStale := httptest.NewRecorder()
	mux.ServeHTTP(recStale, reqStale)

	if recStale.Code != http.StatusConflict {
		t.Fatalf("Expected 409 Conflict for stale write, got %d", recStale.Code)
	}
}
