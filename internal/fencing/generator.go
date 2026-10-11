package fencing

import (
	"context"
	"fmt"
	"strconv"
	"sync"

	"github.com/lockforge/lockforge/internal/etcdclient"
)

// Generator issues monotonically increasing fencing tokens.
type Generator struct {
	client etcdclient.Client
	mu     sync.Mutex
	local  int64
}

// NewGenerator creates a new fencing token generator.
func NewGenerator(client etcdclient.Client) *Generator {
	return &Generator{
		client: client,
		local:  100, // baseline token counter
	}
}

// NextToken generates the next strictly monotonic token for the given resource.
func (g *Generator) NextToken(ctx context.Context, resourceID string) (int64, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	counterKey := fmt.Sprintf("/lockforge/counters/%s", resourceID)
	resp, err := g.client.Get(ctx, counterKey)
	if err != nil {
		// Fallback to local atomic counter if etcd error
		g.local++
		return g.local, nil
	}

	var currentToken int64 = 0
	var modRev int64 = 0
	if len(resp.Kvs) > 0 {
		currentToken, _ = strconv.ParseInt(string(resp.Kvs[0].Value), 10, 64)
		modRev = resp.Kvs[0].ModRevision
	}

	nextToken := currentToken + 1
	if nextToken <= g.local {
		nextToken = g.local + 1
	}

	// Persist next token to etcd
	valStr := strconv.FormatInt(nextToken, 10)
	if modRev == 0 {
		// Try create
		txnResp, err := g.client.Txn(ctx).
			If(etcdclient.CompareCreateRevisionEqual(counterKey, 0)).
			Then(etcdclient.OpPut(counterKey, valStr, 0)).
			Commit(ctx)
		if err == nil && txnResp != nil && txnResp.Succeeded {
			g.local = nextToken
			return nextToken, nil
		}
	} else {
		// Update
		_, _ = g.client.Put(ctx, counterKey, valStr, 0)
	}

	g.local = nextToken
	return nextToken, nil
}
