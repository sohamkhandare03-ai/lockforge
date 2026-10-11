package etcdclient

import (
	"context"
)

// LeaseID represents an etcd lease identifier.
type LeaseID int64

// KeyValue represents a key-value pair with revision metadata.
type KeyValue struct {
	Key            string
	Value          []byte
	CreateRevision int64
	ModRevision    int64
	Version        int64
	Lease          LeaseID
}

// GetResponse represents the result of a Get operation.
type GetResponse struct {
	Kvs   []*KeyValue
	Count int64
}

// PutResponse represents the result of a Put operation.
type PutResponse struct {
	HeaderRevision int64
}

// DeleteResponse represents the result of a Delete operation.
type DeleteResponse struct {
	Deleted int64
}

// CompareOp defines a comparison operator in a transaction.
type CompareOp int

const (
	CompareCreateRevision CompareOp = iota
	CompareModRevision
	CompareValue
	CompareVersion
)

// CompareTarget defines the target of comparison.
type CompareTarget int

const (
	CompareEqual CompareTarget = iota
	CompareGreater
	CompareLess
	CompareNotEqual
)

// Cmp defines a comparison condition for Txn.
type Cmp struct {
	Key     string
	Op      CompareOp
	Target  CompareTarget
	Value   any
}

// Op defines an operation (Put, Get, Delete) within Txn.
type Op struct {
	Type    string
	Key     string
	Val     string
	LeaseID LeaseID
}

// OpPut creates a Put operation.
func OpPut(key, val string, leaseID LeaseID) Op {
	return Op{Type: "PUT", Key: key, Val: val, LeaseID: leaseID}
}

// OpDelete creates a Delete operation.
func OpDelete(key string) Op {
	return Op{Type: "DELETE", Key: key}
}

// OpGet creates a Get operation.
func OpGet(key string) Op {
	return Op{Type: "GET", Key: key}
}

// TxnResponse represents the result of a transaction.
type TxnResponse struct {
	Succeeded bool
	Responses []any
}

// Txn interface for atomic conditional transactions.
type Txn interface {
	If(cs ...Cmp) Txn
	Then(ops ...Op) Txn
	Else(ops ...Op) Txn
	Commit(ctx context.Context) (*TxnResponse, error)
}

// WatchEventType defines the type of watch event.
type WatchEventType int

const (
	WatchEventPut WatchEventType = iota
	WatchEventDelete
)

// WatchEvent represents a key change notification.
type WatchEvent struct {
	Type WatchEventType
	Kv   *KeyValue
	Prev *KeyValue
}

// Client defines the common contract for interacting with etcd.
type Client interface {
	Put(ctx context.Context, key, val string, leaseID LeaseID) (*PutResponse, error)
	Get(ctx context.Context, key string) (*GetResponse, error)
	GetPrefix(ctx context.Context, prefix string) (*GetResponse, error)
	Delete(ctx context.Context, key string) (*DeleteResponse, error)
	Txn(ctx context.Context) Txn
	GrantLease(ctx context.Context, ttlSeconds int64) (LeaseID, error)
	RevokeLease(ctx context.Context, id LeaseID) error
	TimeToLive(ctx context.Context, id LeaseID) (int64, error)
	KeepAliveOnce(ctx context.Context, id LeaseID, extendSeconds int64) (int64, error)
	Watch(ctx context.Context, prefix string) <-chan WatchEvent
	SetPartitionMode(partitioned bool)
	IsPartitioned() bool
	Close() error
}

// CompareCreateRevisionEqual creates a condition checking CreateRevision == expected.
func CompareCreateRevisionEqual(key string, expected int64) Cmp {
	return Cmp{
		Key:    key,
		Op:     CompareCreateRevision,
		Target: CompareEqual,
		Value:  expected,
	}
}

// CompareValueEqual creates a condition checking Value == expected.
func CompareValueEqual(key string, expected string) Cmp {
	return Cmp{
		Key:    key,
		Op:     CompareValue,
		Target: CompareEqual,
		Value:  expected,
	}
}
