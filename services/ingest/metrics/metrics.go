// Outcome counters: stored, duplicate, and each reject reason. Logged periodically; Prometheus /metrics comes later.

package metrics

import (
	"maps"
	"sync"
)

type Counters struct {
	mu     sync.Mutex
	counts map[string]int64
}

func New() *Counters {
	return &Counters{counts: make(map[string]int64)}
}

func (c *Counters) Add(name string, n int64) {
	c.mu.Lock()
	c.counts[name] += n
	c.mu.Unlock()
}

func (c *Counters) Inc(name string) { c.Add(name, 1) }

// Snapshot returns a copy, safe to read while counting continues.
func (c *Counters) Snapshot() map[string]int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return maps.Clone(c.counts)
}
