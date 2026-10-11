package etcdclient

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrQuorumUnavailable = errors.New("etcd: raft quorum lost, network partition active")
	ErrLeaseNotFound     = errors.New("etcd: requested lease not found or expired")
)

type memoryLease struct {
	id        LeaseID
	ttl       int64
	expiresAt time.Time
	keys      map[string]struct{}
}

// MemoryClient implements Client with exact etcd semantics in memory.
type MemoryClient struct {
	mu           sync.RWMutex
	revision     int64
	kvs          map[string]*KeyValue
	leases       map[LeaseID]*memoryLease
	leaseCounter int64
	partitioned  atomic.Bool
	watchers     map[string][]chan WatchEvent
	watchersMu   sync.RWMutex
	stopTicker   chan struct{}
}

// NewMemoryClient creates an in-memory client.
func NewMemoryClient() *MemoryClient {
	c := &MemoryClient{
		kvs:        make(map[string]*KeyValue),
		leases:     make(map[LeaseID]*memoryLease),
		watchers:   make(map[string][]chan WatchEvent),
		stopTicker: make(chan struct{}),
	}
	go c.runLeaseExpirationTicker()
	return c
}

func (c *MemoryClient) SetPartitionMode(partitioned bool) {
	c.partitioned.Store(partitioned)
}

func (c *MemoryClient) IsPartitioned() bool {
	return c.partitioned.Load()
}

func (c *MemoryClient) checkQuorum() error {
	if c.partitioned.Load() {
		return ErrQuorumUnavailable
	}
	return nil
}

func (c *MemoryClient) runLeaseExpirationTicker() {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopTicker:
			return
		case now := <-ticker.C:
			c.expireLeases(now)
		}
	}
}

func (c *MemoryClient) expireLeases(now time.Time) {
	c.mu.Lock()
	var expiredKeys []*KeyValue
	for id, lease := range c.leases {
		if now.After(lease.expiresAt) {
			for key := range lease.keys {
				if kv, ok := c.kvs[key]; ok && kv.Lease == id {
					delete(c.kvs, key)
					c.revision++
					expiredKeys = append(expiredKeys, &KeyValue{
						Key:            kv.Key,
						Value:          kv.Value,
						CreateRevision: kv.CreateRevision,
						ModRevision:    c.revision,
						Version:        kv.Version + 1,
						Lease:          id,
					})
				}
			}
			delete(c.leases, id)
		}
	}
	c.mu.Unlock()

	for _, kv := range expiredKeys {
		c.notifyWatchers(WatchEvent{
			Type: WatchEventDelete,
			Kv:   kv,
			Prev: kv,
		})
	}
}

func (c *MemoryClient) Put(ctx context.Context, key, val string, leaseID LeaseID) (*PutResponse, error) {
	if err := c.checkQuorum(); err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if leaseID > 0 {
		lease, ok := c.leases[leaseID]
		if !ok || time.Now().After(lease.expiresAt) {
			return nil, ErrLeaseNotFound
		}
		lease.keys[key] = struct{}{}
	}

	c.revision++
	prev, exists := c.kvs[key]
	var createRev int64 = c.revision
	var ver int64 = 1
	if exists {
		createRev = prev.CreateRevision
		ver = prev.Version + 1
	}

	kv := &KeyValue{
		Key:            key,
		Value:          []byte(val),
		CreateRevision: createRev,
		ModRevision:    c.revision,
		Version:        ver,
		Lease:          leaseID,
	}
	c.kvs[key] = kv

	go c.notifyWatchers(WatchEvent{
		Type: WatchEventPut,
		Kv:   kv,
		Prev: prev,
	})

	return &PutResponse{HeaderRevision: c.revision}, nil
}

func (c *MemoryClient) Get(ctx context.Context, key string) (*GetResponse, error) {
	if err := c.checkQuorum(); err != nil {
		return nil, err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	kv, ok := c.kvs[key]
	if !ok {
		return &GetResponse{Kvs: nil, Count: 0}, nil
	}

	kvCopy := *kv
	return &GetResponse{
		Kvs:   []*KeyValue{&kvCopy},
		Count: 1,
	}, nil
}

func (c *MemoryClient) GetPrefix(ctx context.Context, prefix string) (*GetResponse, error) {
	if err := c.checkQuorum(); err != nil {
		return nil, err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	var result []*KeyValue
	for k, v := range c.kvs {
		if strings.HasPrefix(k, prefix) {
			kvCopy := *v
			result = append(result, &kvCopy)
		}
	}

	return &GetResponse{
		Kvs:   result,
		Count: int64(len(result)),
	}, nil
}

func (c *MemoryClient) Delete(ctx context.Context, key string) (*DeleteResponse, error) {
	if err := c.checkQuorum(); err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	prev, ok := c.kvs[key]
	if !ok {
		return &DeleteResponse{Deleted: 0}, nil
	}

	c.revision++
	delete(c.kvs, key)

	go c.notifyWatchers(WatchEvent{
		Type: WatchEventDelete,
		Kv: &KeyValue{
			Key:         key,
			ModRevision: c.revision,
		},
		Prev: prev,
	})

	return &DeleteResponse{Deleted: 1}, nil
}

func (c *MemoryClient) GrantLease(ctx context.Context, ttlSeconds int64) (LeaseID, error) {
	if err := c.checkQuorum(); err != nil {
		return 0, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.leaseCounter++
	id := LeaseID(c.leaseCounter)
	c.leases[id] = &memoryLease{
		id:        id,
		ttl:       ttlSeconds,
		expiresAt: time.Now().Add(time.Duration(ttlSeconds) * time.Second),
		keys:      make(map[string]struct{}),
	}
	return id, nil
}

func (c *MemoryClient) RevokeLease(ctx context.Context, id LeaseID) error {
	if err := c.checkQuorum(); err != nil {
		return err
	}

	c.mu.Lock()
	lease, ok := c.leases[id]
	if !ok {
		c.mu.Unlock()
		return ErrLeaseNotFound
	}

	var deletedKvs []*KeyValue
	for key := range lease.keys {
		if kv, exists := c.kvs[key]; exists && kv.Lease == id {
			delete(c.kvs, key)
			c.revision++
			deletedKvs = append(deletedKvs, kv)
		}
	}
	delete(c.leases, id)
	c.mu.Unlock()

	for _, kv := range deletedKvs {
		c.notifyWatchers(WatchEvent{
			Type: WatchEventDelete,
			Kv: &KeyValue{
				Key:         kv.Key,
				ModRevision: c.revision,
			},
			Prev: kv,
		})
	}
	return nil
}

func (c *MemoryClient) TimeToLive(ctx context.Context, id LeaseID) (int64, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	lease, ok := c.leases[id]
	if !ok {
		return 0, ErrLeaseNotFound
	}
	remaining := time.Until(lease.expiresAt).Seconds()
	if remaining < 0 {
		return 0, nil
	}
	return int64(remaining), nil
}

func (c *MemoryClient) KeepAliveOnce(ctx context.Context, id LeaseID, extendSeconds int64) (int64, error) {
	if err := c.checkQuorum(); err != nil {
		return 0, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	lease, ok := c.leases[id]
	if !ok || time.Now().After(lease.expiresAt) {
		return 0, ErrLeaseNotFound
	}

	if extendSeconds <= 0 {
		extendSeconds = lease.ttl
	}
	lease.expiresAt = time.Now().Add(time.Duration(extendSeconds) * time.Second)
	return extendSeconds, nil
}

func (c *MemoryClient) Watch(ctx context.Context, prefix string) <-chan WatchEvent {
	ch := make(chan WatchEvent, 64)
	c.watchersMu.Lock()
	c.watchers[prefix] = append(c.watchers[prefix], ch)
	c.watchersMu.Unlock()

	go func() {
		<-ctx.Done()
		c.watchersMu.Lock()
		defer c.watchersMu.Unlock()
		list := c.watchers[prefix]
		for i, w := range list {
			if w == ch {
				c.watchers[prefix] = append(list[:i], list[i+1:]...)
				close(ch)
				break
			}
		}
	}()

	return ch
}

func (c *MemoryClient) notifyWatchers(event WatchEvent) {
	c.watchersMu.RLock()
	defer c.watchersMu.RUnlock()

	key := ""
	if event.Kv != nil {
		key = event.Kv.Key
	} else if event.Prev != nil {
		key = event.Prev.Key
	}

	for prefix, listeners := range c.watchers {
		if strings.HasPrefix(key, prefix) {
			for _, ch := range listeners {
				select {
				case ch <- event:
				default:
				}
			}
		}
	}
}

func (c *MemoryClient) Txn(ctx context.Context) Txn {
	return &memoryTxn{
		client: c,
	}
}

func (c *MemoryClient) Close() error {
	select {
	case <-c.stopTicker:
	default:
		close(c.stopTicker)
	}
	return nil
}

type memoryTxn struct {
	client *MemoryClient
	cmps   []Cmp
	thens  []Op
	elses  []Op
}

func (t *memoryTxn) If(cs ...Cmp) Txn {
	t.cmps = append(t.cmps, cs...)
	return t
}

func (t *memoryTxn) Then(ops ...Op) Txn {
	t.thens = append(t.thens, ops...)
	return t
}

func (t *memoryTxn) Else(ops ...Op) Txn {
	t.elses = append(t.elses, ops...)
	return t
}

func (t *memoryTxn) Commit(ctx context.Context) (*TxnResponse, error) {
	if err := t.client.checkQuorum(); err != nil {
		return nil, err
	}

	t.client.mu.Lock()
	defer t.client.mu.Unlock()

	success := true
	for _, cmp := range t.cmps {
		kv, exists := t.client.kvs[cmp.Key]
		switch cmp.Op {
		case CompareCreateRevision:
			expected := cmp.Value.(int64)
			actual := int64(0)
			if exists {
				actual = kv.CreateRevision
			}
			if cmp.Target == CompareEqual && actual != expected {
				success = false
			}
		case CompareValue:
			expected := cmp.Value.(string)
			actual := ""
			if exists {
				actual = string(kv.Value)
			}
			if cmp.Target == CompareEqual && actual != expected {
				success = false
			}
		}
		if !success {
			break
		}
	}

	opsToExecute := t.thens
	if !success {
		opsToExecute = t.elses
	}

	var responses []any
	for _, op := range opsToExecute {
		switch op.Type {
		case "PUT":
			t.client.revision++
			prev, exists := t.client.kvs[op.Key]
			var createRev int64 = t.client.revision
			var ver int64 = 1
			if exists {
				createRev = prev.CreateRevision
				ver = prev.Version + 1
			}

			if op.LeaseID > 0 {
				if lease, ok := t.client.leases[op.LeaseID]; ok {
					lease.keys[op.Key] = struct{}{}
				}
			}

			newKv := &KeyValue{
				Key:            op.Key,
				Value:          []byte(op.Val),
				CreateRevision: createRev,
				ModRevision:    t.client.revision,
				Version:        ver,
				Lease:          op.LeaseID,
			}
			t.client.kvs[op.Key] = newKv
			responses = append(responses, newKv)

			go t.client.notifyWatchers(WatchEvent{
				Type: WatchEventPut,
				Kv:   newKv,
				Prev: prev,
			})

		case "DELETE":
			prev, exists := t.client.kvs[op.Key]
			if exists {
				t.client.revision++
				delete(t.client.kvs, op.Key)
				responses = append(responses, true)
				go t.client.notifyWatchers(WatchEvent{
					Type: WatchEventDelete,
					Kv: &KeyValue{
						Key:         op.Key,
						ModRevision: t.client.revision,
					},
					Prev: prev,
				})
			} else {
				responses = append(responses, false)
			}

		case "GET":
			if kv, exists := t.client.kvs[op.Key]; exists {
				kvCopy := *kv
				responses = append(responses, &kvCopy)
			} else {
				responses = append(responses, nil)
			}
		}
	}

	return &TxnResponse{
		Succeeded: success,
		Responses: responses,
	}, nil
}
