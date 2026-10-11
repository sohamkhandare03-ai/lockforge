package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"

	"github.com/lockforge/lockforge/internal/events"
	"github.com/lockforge/lockforge/internal/protectedresource"
)

func main() {
	port := flag.Int("port", 8081, "Port for simulated protected resource HTTP service")
	flag.Parse()

	log.Printf("Starting Protected Resource Guard Service on :%d ...", *port)
	pr := protectedresource.NewProtectedResource(events.NewRingBuffer(100))

	mux := http.NewServeMux()
	mux.HandleFunc("/write", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			ResourceID   string `json:"resource_id"`
			ClientID     string `json:"client_id"`
			FencingToken int64  `json:"fencing_token"`
			Data         string `json:"data"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}

		res, err := pr.Write(r.Context(), req.ResourceID, req.ClientID, req.FencingToken, req.Data)
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			w.WriteHeader(http.StatusConflict)
		} else {
			w.WriteHeader(http.StatusOK)
		}
		_ = json.NewEncoder(w).Encode(res)
	})

	mux.HandleFunc("/record", func(w http.ResponseWriter, r *http.Request) {
		resID := r.URL.Query().Get("resource")
		rec, found := pr.GetRecord(resID)
		w.Header().Set("Content-Type", "application/json")
		if !found {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "resource not found"})
			return
		}
		_ = json.NewEncoder(w).Encode(rec)
	})

	if err := http.ListenAndServe(fmt.Sprintf(":%d", *port), mux); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
