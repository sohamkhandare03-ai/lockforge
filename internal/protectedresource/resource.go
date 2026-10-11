package protectedresource

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/lockforge/lockforge/internal/events"
	pb "github.com/lockforge/lockforge/proto"
)

var (
	ErrStaleWriteRejected = errors.New("stale write rejected: fencing token violation")
)

// ResourceRecord represents the stored state of a protected resource.
type ResourceRecord struct {
	ResourceID          string    `json:"resource_id"`
	Data                string    `json:"data"`
	LastSeenToken       int64     `json:"last_seen_token"`
	LastWriterClientID  string    `json:"last_writer_client_id"`
	LastUpdatedAt       time.Time `json:"last_updated_at"`
}

// WriteResult contains details of the write operation.
type WriteResult struct {
	Accepted      bool   `json:"accepted"`
	ResourceID    string `json:"resource_id"`
	Token         int64  `json:"token"`
	LastSeenToken int64  `json:"last_seen_token"`
	Message       string `json:"message"`
}

// ProtectedResource simulates a guarded storage system validating fencing tokens.
type ProtectedResource struct {
	mu      sync.RWMutex
	records map[string]*ResourceRecord
	events  *events.RingBuffer
}

// NewProtectedResource initializes the simulated protected resource.
func NewProtectedResource(eventBuffer *events.RingBuffer) *ProtectedResource {
	return &ProtectedResource{
		records: make(map[string]*ResourceRecord),
		events:  eventBuffer,
	}
}

// Write checks if incoming token > last_seen_token. Rejects stale tokens!
func (pr *ProtectedResource) Write(ctx context.Context, resourceID, clientID string, fencingToken int64, data string) (*WriteResult, error) {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	rec, exists := pr.records[resourceID]
	if !exists {
		rec = &ResourceRecord{
			ResourceID:    resourceID,
			LastSeenToken: 0,
		}
		pr.records[resourceID] = rec
	}

	// Martin Kleppmann Fencing Token Safety Check:
	// A write is only valid if its fencing token is strictly greater than any previously processed token.
	if fencingToken <= rec.LastSeenToken {
		msg := fmt.Sprintf("Fencing violation on '%s': incoming token %d <= last seen token %d (from client '%s')",
			resourceID, fencingToken, rec.LastSeenToken, rec.LastWriterClientID)

		if pr.events != nil {
			pr.events.Publish(
				pb.LockEventType_LOCK_EVENT_TYPE_STALE_WRITE_REJECTED,
				resourceID,
				clientID,
				fencingToken,
				0,
				msg,
			)
		}

		return &WriteResult{
			Accepted:      false,
			ResourceID:    resourceID,
			Token:         fencingToken,
			LastSeenToken: rec.LastSeenToken,
			Message:       msg,
		}, ErrStaleWriteRejected
	}

	// Accept write
	rec.Data = data
	rec.LastSeenToken = fencingToken
	rec.LastWriterClientID = clientID
	rec.LastUpdatedAt = time.Now()

	msg := fmt.Sprintf("Write accepted on '%s' from '%s' with token %d (previous was %d)",
		resourceID, clientID, fencingToken, rec.LastSeenToken)

	if pr.events != nil {
		pr.events.Publish(
			pb.LockEventType_LOCK_EVENT_TYPE_WRITE_ACCEPTED,
			resourceID,
			clientID,
			fencingToken,
			0,
			msg,
		)
	}

	return &WriteResult{
		Accepted:      true,
		ResourceID:    resourceID,
		Token:         fencingToken,
		LastSeenToken: fencingToken,
		Message:       msg,
	}, nil
}

// GetRecord returns the stored record for a resource.
func (pr *ProtectedResource) GetRecord(resourceID string) (*ResourceRecord, bool) {
	pr.mu.RLock()
	defer pr.mu.RUnlock()

	rec, ok := pr.records[resourceID]
	if !ok {
		return nil, false
	}
	copyRec := *rec
	return &copyRec, true
}
