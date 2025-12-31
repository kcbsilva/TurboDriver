package api

import (
	"sync"
	"time"
)

// bucketCounter accumulates counts for latency buckets.
type bucketCounter struct {
	mu      sync.Mutex
	buckets map[float64]int64
}

func newBucketCounter(buckets map[float64]int64) bucketCounter {
	return bucketCounter{buckets: buckets}
}

func (c *bucketCounter) observe(d time.Duration) {
	secs := d.Seconds()
	c.mu.Lock()
	defer c.mu.Unlock()
	for le := range c.buckets {
		if secs <= le {
			c.buckets[le]++
		}
	}
}

func (c *bucketCounter) snapshot() map[float64]int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[float64]int64, len(c.buckets))
	for k, v := range c.buckets {
		out[k] = v
	}
	return out
}
