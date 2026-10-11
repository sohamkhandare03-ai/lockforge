package events

import (
	"fmt"
	"sync"
	"time"

	pb "github.com/lockforge/lockforge/proto"
)

// RingBuffer stores a bounded rolling history of lock events.
type RingBuffer struct {
	mu          sync.RWMutex
	capacity    int
	events      []*pb.LockEvent
	head        int
	count       int
	subscribers map[chan *pb.LockEvent]struct{}
	counter     int64
}

// NewRingBuffer initializes an event ring buffer.
func NewRingBuffer(capacity int) *RingBuffer {
	if capacity <= 0 {
		capacity = 500
	}
	return &RingBuffer{
		capacity:    capacity,
		events:      make([]*pb.LockEvent, capacity),
		subscribers: make(map[chan *pb.LockEvent]struct{}),
	}
}

// Publish records a new event and broadcasts it to all live streaming subscribers.
func (r *RingBuffer) Publish(eventType pb.LockEventType, resourceID, clientID string, fencingToken, leaseID int64, details string) *pb.LockEvent {
	r.mu.Lock()
	r.counter++
	ev := &pb.LockEvent{
		EventId:          fmt.Sprintf("ev-%d", r.counter),
		EventType:        eventType,
		ResourceId:       resourceID,
		ClientId:         clientID,
		FencingToken:     fencingToken,
		LeaseId:          leaseID,
		TimestampUnixMs:  time.Now().UnixMilli(),
		Details:          details,
	}

	idx := (r.head + r.count) % r.capacity
	if r.count < r.capacity {
		r.count++
	} else {
		r.head = (r.head + 1) % r.capacity
	}
	r.events[idx] = ev

	// Broadcast to subscribers
	for sub := range r.subscribers {
		select {
		case sub <- ev:
		default:
			// Non-blocking drop if channel buffer full
		}
	}
	r.mu.Unlock()

	return ev
}

// GetRecent returns up to limit recent events in reverse chronological order (newest first).
func (r *RingBuffer) GetRecent(limit int) []*pb.LockEvent {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if limit <= 0 || limit > r.count {
		limit = r.count
	}

	result := make([]*pb.LockEvent, limit)
	for i := 0; i < limit; i++ {
		pos := (r.head + r.count - 1 - i) % r.capacity
		result[i] = r.events[pos]
	}
	return result
}

// Subscribe returns a channel receiving newly published events and an unsubscribe func.
func (r *RingBuffer) Subscribe() (<-chan *pb.LockEvent, func()) {
	r.mu.Lock()
	defer r.mu.Unlock()

	ch := make(chan *pb.LockEvent, 64)
	r.subscribers[ch] = struct{}{}

	unsubscribe := func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		delete(r.subscribers, ch)
		close(ch)
	}

	return ch, unsubscribe
}
