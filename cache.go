// File: cache.go

package grpolicy

import (
	"container/list"
	"sync"
)

// defaultCacheSize is used whenever WithCacheSize is given a value <= 0
// (including when it is never called at all). A non-positive size is never
// silently treated as "unbounded" — that must be requested explicitly via
// WithUnboundedCache, so a zero-value misconfiguration can never cause
// unbounded memory growth.
const defaultCacheSize = 512

// policyCache is a size-bounded, least-recently-used cache of compiled
// policies keyed by their source expression string. All mutation is behind
// a single mutex — no lock-free cleverness, no sync.Map, no double-checked
// locking — deliberately the simplest thing to reason about; its
// correctness under real concurrent Compile/Evaluate load is verified by a
// dedicated -race test (testConcurrentCompileEvaluateStress in
// contract_engine_test.go), not assumed from a static read of this file.
type policyCache struct {
	mu sync.Mutex

	// capacity <= 0 means unbounded (only reachable via WithUnboundedCache;
	// NewEngine itself never constructs a policyCache with capacity <= 0
	// unless that option was used).
	capacity int
	items    map[string]*list.Element
	order    *list.List // Front() = most recently used
}

type cacheEntry struct {
	key    string
	policy *compiledPolicy
}

func newPolicyCache(capacity int) *policyCache {
	return &policyCache{
		capacity: capacity,
		items:    make(map[string]*list.Element),
		order:    list.New(),
	}
}

func (c *policyCache) get(key string) (*compiledPolicy, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		return nil, false
	}
	c.order.MoveToFront(el)
	return el.Value.(*cacheEntry).policy, true
}

func (c *policyCache) put(key string, p *compiledPolicy) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if el, ok := c.items[key]; ok {
		el.Value.(*cacheEntry).policy = p
		c.order.MoveToFront(el)
		return
	}

	el := c.order.PushFront(&cacheEntry{key: key, policy: p})
	c.items[key] = el

	if c.capacity > 0 {
		for c.order.Len() > c.capacity {
			back := c.order.Back()
			if back == nil {
				break
			}
			c.order.Remove(back)
			delete(c.items, back.Value.(*cacheEntry).key)
		}
	}
}
