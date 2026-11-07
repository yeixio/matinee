package api

import (
	"net/http"
	"net/http/pprof"
	"os"
	"runtime/metrics"
)

// pprof serves Go's profiler (CPU, heap, goroutines and the rest) under
// /debug/pprof/, with the metrics token as a bearer token, for chasing
// slowness and leaks on a real server. It's off while metrics are off.
func (s *Server) pprof(w http.ResponseWriter, r *http.Request) {
	if !s.metricsAllowed(w, r) {
		return
	}
	switch name := r.PathValue("profile"); name {
	case "":
		pprof.Index(w, r)
	case "cmdline":
		// The command line can hold settings; it's not needed for profiling.
		http.NotFound(w, r)
	case "profile":
		pprof.Profile(w, r)
	case "symbol":
		pprof.Symbol(w, r)
	case "trace":
		pprof.Trace(w, r)
	default:
		pprof.Handler(name).ServeHTTP(w, r)
	}
}

var runtimeSamples = []metrics.Sample{
	{Name: "/memory/classes/heap/objects:bytes"},
	{Name: "/gc/cycles/total:gc-cycles"},
}

// writeRuntimeMetrics adds the Go runtime's numbers that show leaks and
// stalls: live heap, garbage collections, open files.
func writeRuntimeMetrics(b *metricsWriter) {
	samples := make([]metrics.Sample, len(runtimeSamples))
	copy(samples, runtimeSamples)
	metrics.Read(samples)
	if v := samples[0].Value; v.Kind() == metrics.KindUint64 {
		b.gauge("matinee_heap_live_bytes", "Memory held by live Go objects in the server; one that only grows is a leak.", float64(v.Uint64()))
	}
	if v := samples[1].Value; v.Kind() == metrics.KindUint64 {
		b.help("matinee_gc_cycles_total", "counter", "Go garbage collections since the server started.")
		b.sample("matinee_gc_cycles_total", float64(v.Uint64()))
	}
	if n, ok := openFiles(); ok {
		b.gauge("matinee_open_files", "Files and connections the server has open; one that only grows is a leak.", float64(n))
	}
}

// openFiles counts the process's open file descriptors, where the system
// says (Linux).
func openFiles() (int, bool) {
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return 0, false
	}
	return len(entries), true
}
