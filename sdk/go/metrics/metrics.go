package metrics

import (
	"sync"
	"sync/atomic"
	"time"
)

type Recorder struct {
	mu sync.Mutex
	m  map[string]*MethodMetrics
}

type MethodMetrics struct {
	Requests    atomic.Int64
	Successes   atomic.Int64
	Failures    atomic.Int64
	TotalDuration atomic.Int64
}

func NewRecorder() *Recorder {
	return &Recorder{m: make(map[string]*MethodMetrics)}
}

func (r *Recorder) Record(method string, duration time.Duration, success bool) {
	r.mu.Lock()
	m, ok := r.m[method]
	if !ok {
		m = &MethodMetrics{}
		r.m[method] = m
	}
	r.mu.Unlock()

	m.Requests.Add(1)
	if success {
		m.Successes.Add(1)
	} else {
		m.Failures.Add(1)
	}
	m.TotalDuration.Add(duration.Milliseconds())
}

func (r *Recorder) Get(method string) (requests, successes, failures int64, avgDurationMs float64) {
	r.mu.Lock()
	m, ok := r.m[method]
	r.mu.Unlock()

	if !ok {
		return 0, 0, 0, 0
	}

	requests = m.Requests.Load()
	successes = m.Successes.Load()
	failures = m.Failures.Load()

	total := m.TotalDuration.Load()
	if requests > 0 {
		avgDurationMs = float64(total) / float64(requests)
	}

	return
}

func (r *Recorder) Snapshot() map[string]MethodSnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()

	snap := make(map[string]MethodSnapshot)
	for k, v := range r.m {
		requests := v.Requests.Load()
		avgMs := 0.0
		if requests > 0 {
			avgMs = float64(v.TotalDuration.Load()) / float64(requests)
		}
		snap[k] = MethodSnapshot{
			Requests:      requests,
			Successes:     v.Successes.Load(),
			Failures:      v.Failures.Load(),
			AvgDurationMs: avgMs,
		}
	}
	return snap
}

type MethodSnapshot struct {
	Requests      int64
	Successes     int64
	Failures      int64
	AvgDurationMs float64
}
