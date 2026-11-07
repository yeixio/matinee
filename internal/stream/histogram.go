package stream

import (
	"slices"
	"sync"
)

// Histogram counts durations (in seconds) into buckets, for Prometheus.
type Histogram struct {
	mu     sync.Mutex
	bounds []float64
	counts []uint64 // per bucket, not cumulative; the last is +Inf
	sum    float64
}

// NewHistogram makes a histogram with these bucket upper bounds, ascending.
func NewHistogram(bounds ...float64) *Histogram {
	return &Histogram{bounds: bounds, counts: make([]uint64, len(bounds)+1)}
}

// Observe adds one duration.
func (h *Histogram) Observe(v float64) {
	i, _ := slices.BinarySearch(h.bounds, v)
	h.mu.Lock()
	h.counts[i]++
	h.sum += v
	h.mu.Unlock()
}

// Snapshot returns the bucket bounds, the cumulative count at or under each
// (the last entry is every observation, +Inf), and the sum.
func (h *Histogram) Snapshot() (bounds []float64, cumulative []uint64, sum float64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	cumulative = make([]uint64, len(h.counts))
	var n uint64
	for i, c := range h.counts {
		n += c
		cumulative[i] = n
	}
	return h.bounds, cumulative, h.sum
}
