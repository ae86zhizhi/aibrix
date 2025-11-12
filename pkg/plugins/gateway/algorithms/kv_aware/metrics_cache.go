/*
Copyright 2024 The Aibrix Team.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package kvaware

import (
	"sync"
	"time"

	"k8s.io/klog/v2"
)

// metricsCacheEntry represents a single cached metrics entry
type metricsCacheEntry struct {
	metrics   PodMetrics
	timestamp time.Time
}

// metricsCache provides local caching of pod metrics with TTL
type metricsCache struct {
	mu      sync.RWMutex
	entries map[string]*metricsCacheEntry
	ttl     time.Duration
}

// newMetricsCache creates a new metrics cache with the specified TTL
func newMetricsCache(ttl time.Duration) *metricsCache {
	return &metricsCache{
		entries: make(map[string]*metricsCacheEntry),
		ttl:     ttl,
	}
}

// get retrieves cached metrics if available and not expired
func (c *metricsCache) get(key string) (PodMetrics, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, exists := c.entries[key]
	if !exists {
		return PodMetrics{}, false
	}

	// Check if entry is expired
	if time.Since(entry.timestamp) > c.ttl {
		klog.V(6).Infof("Metrics cache entry for %s expired", key)
		return PodMetrics{}, false
	}

	klog.V(6).Infof("Metrics cache hit for %s (age: %v)", key, time.Since(entry.timestamp))
	return entry.metrics, true
}

// put stores metrics in the cache
func (c *metricsCache) put(key string, metrics PodMetrics) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries[key] = &metricsCacheEntry{
		metrics:   metrics,
		timestamp: time.Now(),
	}

	klog.V(6).Infof("Metrics cached for %s", key)
}

// cleanup removes expired entries from the cache
func (c *metricsCache) cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	expired := 0

	for key, entry := range c.entries {
		if now.Sub(entry.timestamp) > c.ttl {
			delete(c.entries, key)
			expired++
		}
	}

	if expired > 0 {
		klog.V(5).Infof("Metrics cache cleanup: removed %d expired entries", expired)
	}
}

// startCleanupLoop starts a background goroutine to periodically clean up expired entries
func (c *metricsCache) startCleanupLoop(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for range ticker.C {
			c.cleanup()
		}
	}()

	klog.V(4).Infof("Metrics cache cleanup loop started (interval: %v, TTL: %v)", interval, c.ttl)
}

// size returns the current number of cached entries
func (c *metricsCache) size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// clear removes all entries from the cache
func (c *metricsCache) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]*metricsCacheEntry)
	klog.V(5).Info("Metrics cache cleared")
}
