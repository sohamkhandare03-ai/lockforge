package etcdclient

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

// RealClient wraps the official etcd clientv3.
type RealClient struct {
	cli         *clientv3.Client
	partitioned atomic.Bool
}

// NewRealClient connects to etcd endpoints.
func NewRealClient(endpoints []string, dialTimeout time.Duration) (*RealClient, error) {
	if dialTimeout <= 0 {
		dialTimeout = 3 * time.Second
	}
	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   endpoints,
		DialTimeout: dialTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to etcd: %w", err)
	}

	return &RealClient{
		cli: cli,
	}, nil
}

func (r *RealClient) SetPartitionMode(partitioned bool) {
	r.partitioned.Store(partitioned)
}

func (r *RealClient) IsPartitioned() bool {
	return r.partitioned.Load()
}

func (r *RealClient) checkQuorum() error {
	if r.partitioned.Load() {
		return ErrQuorumUnavailable
	}
	return nil
}

func (r *RealClient) Put(ctx context.Context, key, val string, leaseID LeaseID) (*PutResponse, error) {
	if err := r.checkQuorum(); err != nil {
		return nil, err
	}
	var opts []clientv3.OpOption
	if leaseID > 0 {
		opts = append(opts, clientv3.WithLease(clientv3.LeaseID(leaseID)))
	}
	resp, err := r.cli.Put(ctx, key, val, opts...)
	if err != nil {
		return nil, err
	}
	return &PutResponse{HeaderRevision: resp.Header.Revision}, nil
}

func (r *RealClient) Get(ctx context.Context, key string) (*GetResponse, error) {
	if err := r.checkQuorum(); err != nil {
		return nil, err
	}
	resp, err := r.cli.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	res := &GetResponse{Count: resp.Count}
	for _, kv := range resp.Kvs {
		res.Kvs = append(res.Kvs, &KeyValue{
			Key:            string(kv.Key),
			Value:          kv.Value,
			CreateRevision: kv.CreateRevision,
			ModRevision:    kv.ModRevision,
			Version:        kv.Version,
			Lease:          LeaseID(kv.Lease),
		})
	}
	return res, nil
}

func (r *RealClient) GetPrefix(ctx context.Context, prefix string) (*GetResponse, error) {
	if err := r.checkQuorum(); err != nil {
		return nil, err
	}
	resp, err := r.cli.Get(ctx, prefix, clientv3.WithPrefix())
	if err != nil {
		return nil, err
	}
	res := &GetResponse{Count: resp.Count}
	for _, kv := range resp.Kvs {
		res.Kvs = append(res.Kvs, &KeyValue{
			Key:            string(kv.Key),
			Value:          kv.Value,
			CreateRevision: kv.CreateRevision,
			ModRevision:    kv.ModRevision,
			Version:        kv.Version,
			Lease:          LeaseID(kv.Lease),
		})
	}
	return res, nil
}

func (r *RealClient) Delete(ctx context.Context, key string) (*DeleteResponse, error) {
	if err := r.checkQuorum(); err != nil {
		return nil, err
	}
	resp, err := r.cli.Delete(ctx, key)
	if err != nil {
		return nil, err
	}
	return &DeleteResponse{Deleted: resp.Deleted}, nil
}

func (r *RealClient) GrantLease(ctx context.Context, ttlSeconds int64) (LeaseID, error) {
	if err := r.checkQuorum(); err != nil {
		return 0, err
	}
	resp, err := r.cli.Grant(ctx, ttlSeconds)
	if err != nil {
		return 0, err
	}
	return LeaseID(resp.ID), nil
}

func (r *RealClient) RevokeLease(ctx context.Context, id LeaseID) error {
	if err := r.checkQuorum(); err != nil {
		return err
	}
	_, err := r.cli.Revoke(ctx, clientv3.LeaseID(id))
	return err
}

func (r *RealClient) TimeToLive(ctx context.Context, id LeaseID) (int64, error) {
	resp, err := r.cli.TimeToLive(ctx, clientv3.LeaseID(id))
	if err != nil {
		return 0, err
	}
	return resp.TTL, nil
}

func (r *RealClient) KeepAliveOnce(ctx context.Context, id LeaseID, extendSeconds int64) (int64, error) {
	if err := r.checkQuorum(); err != nil {
		return 0, err
	}
	resp, err := r.cli.KeepAliveOnce(ctx, clientv3.LeaseID(id))
	if err != nil {
		return 0, err
	}
	return resp.TTL, nil
}

func (r *RealClient) Watch(ctx context.Context, prefix string) <-chan WatchEvent {
	out := make(chan WatchEvent, 64)
	rch := r.cli.Watch(ctx, prefix, clientv3.WithPrefix(), clientv3.WithPrevKV())

	go func() {
		defer close(out)
		for wresp := range rch {
			for _, ev := range wresp.Events {
				var evType WatchEventType
				if ev.Type == clientv3.EventTypePut {
					evType = WatchEventPut
				} else {
					evType = WatchEventDelete
				}

				var kv *KeyValue
				if ev.Kv != nil {
					kv = &KeyValue{
						Key:            string(ev.Kv.Key),
						Value:          ev.Kv.Value,
						CreateRevision: ev.Kv.CreateRevision,
						ModRevision:    ev.Kv.ModRevision,
						Version:        ev.Kv.Version,
						Lease:          LeaseID(ev.Kv.Lease),
					}
				}

				var prev *KeyValue
				if ev.PrevKv != nil {
					prev = &KeyValue{
						Key:            string(ev.PrevKv.Key),
						Value:          ev.PrevKv.Value,
						CreateRevision: ev.PrevKv.CreateRevision,
						ModRevision:    ev.PrevKv.ModRevision,
						Version:        ev.PrevKv.Version,
						Lease:          LeaseID(ev.PrevKv.Lease),
					}
				}

				select {
				case out <- WatchEvent{Type: evType, Kv: kv, Prev: prev}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return out
}

func (r *RealClient) Txn(ctx context.Context) Txn {
	return &realTxn{
		cli:    r.cli,
		parent: r,
	}
}

func (r *RealClient) Close() error {
	return r.cli.Close()
}

type realTxn struct {
	cli    *clientv3.Client
	parent *RealClient
	cmps   []Cmp
	thens  []Op
	elses  []Op
}

func (t *realTxn) If(cs ...Cmp) Txn {
	t.cmps = append(t.cmps, cs...)
	return t
}

func (t *realTxn) Then(ops ...Op) Txn {
	t.thens = append(t.thens, ops...)
	return t
}

func (t *realTxn) Else(ops ...Op) Txn {
	t.elses = append(t.elses, ops...)
	return t
}

func (t *realTxn) Commit(ctx context.Context) (*TxnResponse, error) {
	if err := t.parent.checkQuorum(); err != nil {
		return nil, err
	}

	txn := t.cli.Txn(ctx)
	var etcdCmps []clientv3.Cmp
	for _, c := range t.cmps {
		if c.Op == CompareCreateRevision && c.Target == CompareEqual {
			etcdCmps = append(etcdCmps, clientv3.Compare(clientv3.CreateRevision(c.Key), "=", c.Value.(int64)))
		} else if c.Op == CompareValue && c.Target == CompareEqual {
			etcdCmps = append(etcdCmps, clientv3.Compare(clientv3.Value(c.Key), "=", c.Value.(string)))
		}
	}

	var thenOps []clientv3.Op
	for _, op := range t.thens {
		switch op.Type {
		case "PUT":
			var opts []clientv3.OpOption
			if op.LeaseID > 0 {
				opts = append(opts, clientv3.WithLease(clientv3.LeaseID(op.LeaseID)))
			}
			thenOps = append(thenOps, clientv3.OpPut(op.Key, op.Val, opts...))
		case "DELETE":
			thenOps = append(thenOps, clientv3.OpDelete(op.Key))
		case "GET":
			thenOps = append(thenOps, clientv3.OpGet(op.Key))
		}
	}

	var elseOps []clientv3.Op
	for _, op := range t.elses {
		switch op.Type {
		case "PUT":
			var opts []clientv3.OpOption
			if op.LeaseID > 0 {
				opts = append(opts, clientv3.WithLease(clientv3.LeaseID(op.LeaseID)))
			}
			elseOps = append(elseOps, clientv3.OpPut(op.Key, op.Val, opts...))
		case "DELETE":
			elseOps = append(elseOps, clientv3.OpDelete(op.Key))
		case "GET":
			elseOps = append(elseOps, clientv3.OpGet(op.Key))
		}
	}

	resp, err := txn.If(etcdCmps...).Then(thenOps...).Else(elseOps...).Commit()
	if err != nil {
		return nil, err
	}

	return &TxnResponse{
		Succeeded: resp.Succeeded,
	}, nil
}
