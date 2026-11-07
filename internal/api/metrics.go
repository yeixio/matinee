package api

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/yeixio/matinee/internal/alerts"
	"github.com/yeixio/matinee/internal/stream"
	"github.com/yeixio/matinee/internal/version"
)

// Prometheus metrics (GET /metrics): what Server health shows, for
// Prometheus, Grafana and the like on the admin's own network. They're off
// until an admin turns them on under Settings → Server health → Alerts,
// which makes a token for Prometheus to send as a bearer token.

const settingMetricsToken = "metrics_token"

type metricsView struct {
	On    bool   `json:"on"`
	Token string `json:"token,omitempty"`
	Path  string `json:"path"`
}

func (s *Server) getMetricsSettings(w http.ResponseWriter, r *http.Request) {
	tok, err := s.Store.Setting(r.Context(), settingMetricsToken)
	if err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, metricsView{On: tok != "", Token: tok, Path: "/metrics"})
}

// newMetricsToken turns metrics on with a new token, or changes the token.
func (s *Server) newMetricsToken(w http.ResponseWriter, r *http.Request) {
	b := make([]byte, 24)
	rand.Read(b)
	if err := s.Store.SetSetting(r.Context(), settingMetricsToken, hex.EncodeToString(b)); err != nil {
		s.serverError(w, err)
		return
	}
	s.getMetricsSettings(w, r)
}

func (s *Server) deleteMetricsToken(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.SetSetting(r.Context(), settingMetricsToken, ""); err != nil {
		s.serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// metricsAllowed checks the metrics token, which /metrics and /stats take
// as a bearer token, and answers when it's missing or wrong.
func (s *Server) metricsAllowed(w http.ResponseWriter, r *http.Request) bool {
	tok, err := s.Store.Setting(r.Context(), settingMetricsToken)
	if err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return false
	}
	if tok == "" {
		http.Error(w, "metrics are off; turn them on in Settings → Server health → Alerts", http.StatusNotFound)
		return false
	}
	got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || subtle.ConstantTimeCompare([]byte(strings.TrimSpace(got)), []byte(tok)) != 1 {
		w.Header().Set("WWW-Authenticate", `Bearer realm="matinee"`)
		http.Error(w, "send the metrics token as a bearer token", http.StatusUnauthorized)
		return false
	}
	return true
}

// metrics answers Prometheus in its text format.
func (s *Server) metrics(w http.ResponseWriter, r *http.Request) {
	if !s.metricsAllowed(w, r) {
		return
	}

	var b metricsWriter
	m := s.health()
	sys := m.System()
	b.gauge("matinee_build_info", "The server's version.", 1, "version", version.Version, "goversion", runtime.Version())
	b.gauge("matinee_start_time_seconds", "When the server started, in seconds since 1970.", float64(sys.Started)/1000)
	b.gauge("matinee_cpu_percent", "How much of the machine's processors (or the container's share) Matinee uses, 0 to 100.", sys.CPU)
	if sys.HostCPU != nil {
		b.gauge("matinee_host_cpu_percent", "How much of the whole machine's processors are in use, 0 to 100.", *sys.HostCPU)
	}
	b.gauge("matinee_memory_bytes", "Memory Matinee uses.", float64(sys.Memory))
	if sys.MemLimit > 0 {
		b.gauge("matinee_memory_limit_bytes", "The container's memory limit.", float64(sys.MemLimit))
	}
	b.gauge("matinee_goroutines", "Goroutines in the server process.", float64(sys.Goroutines))
	b.counter("matinee_sent_bytes_total", "Bytes sent since the server started.", float64(sys.BytesOut))
	b.counter("matinee_http_requests_total", "Requests answered since the server started.", float64(sys.Requests))
	b.counter("matinee_http_server_errors_total", "Requests that failed on the server since it started.", float64(sys.ServerErrors))
	b.counter("matinee_log_warnings_total", "Warnings logged since the server started.", float64(sys.Warnings))
	b.counter("matinee_log_errors_total", "Errors logged since the server started.", float64(sys.Errors))

	playing, transcodes := s.StreamCounts()
	b.gauge("matinee_streams_playing", "Players playing now.", float64(playing))
	b.gauge("matinee_transcodes_active", "Transcodes running now.", float64(transcodes))
	if s.Transcoder != nil {
		started, failed := s.Transcoder.Counts()
		b.counter("matinee_transcodes_started_total", "Transcodes started since the server started.", float64(started))
		b.counter("matinee_transcodes_failed_total", "Transcodes that failed since the server started.", float64(failed))
		superseded, procs := s.Transcoder.Stats()
		b.counter("matinee_transcodes_superseded_total", "Transcodes stopped because the player seeked elsewhere before or while they ran.", float64(superseded))
		b.gauge("matinee_ffmpeg_processes", "FFmpeg processes Matinee runs for playback now, including stopped ones still exiting. More than matinee_transcodes_active for long means FFmpeg isn't stopping.", float64(procs))
		slow := 0
		for _, t := range s.Transcoder.Sessions() {
			// Speed is known once a few pieces are made.
			if t.Speed > 0 && t.Speed < 1 && time.Since(time.UnixMilli(t.Started)) > 30*time.Second {
				slow++
			}
		}
		b.gauge("matinee_transcodes_slow", "Transcodes making video slower than it plays, so their players will buffer.", float64(slow))
		b.histogram("matinee_transcode_start_seconds", "How long players wait for a transcode to start (a play or a far seek), until its first piece is ready.", s.Transcoder.StartTimes)
		b.histogram("matinee_segment_wait_seconds", "How long players wait for each transcoded piece they ask for. Long waits are buffering.", s.Transcoder.SegmentWaits)
	}
	writeRuntimeMetrics(&b)

	libs, _ := s.Store.Libraries(r.Context())
	disks := s.disks(s.storageUses(libs))
	b.help("matinee_disk_total_bytes", "gauge", "Size of each disk the server uses, by the first folder on it.")
	for _, d := range disks {
		if d.Reachable {
			b.sample("matinee_disk_total_bytes", float64(d.Total), "path", d.Uses[0].Path, "use", diskUses(d))
		}
	}
	b.help("matinee_disk_free_bytes", "gauge", "Free space on each disk the server uses.")
	for _, d := range disks {
		if d.Reachable {
			b.sample("matinee_disk_free_bytes", float64(d.Free), "path", d.Uses[0].Path, "use", diskUses(d))
		}
	}
	b.help("matinee_disk_reachable", "gauge", "1 when the folder can be read, 0 when it can't (such as a network share that dropped).")
	for _, d := range disks {
		v := 0.0
		if d.Reachable {
			v = 1
		}
		b.sample("matinee_disk_reachable", v, "path", d.Uses[0].Path, "use", diskUses(d))
	}
	b.gauge("matinee_libraries", "Libraries on the server.", float64(len(libs)))

	firing := s.alerts().Firing()
	b.help("matinee_alerts_firing", "gauge", "Alerts that are true now, by rule.")
	counts := map[string]int{}
	for _, a := range firing {
		counts[a.Rule]++
	}
	for _, rule := range []string{alerts.RuleDisk, alerts.RuleFolder, alerts.RuleErrors, alerts.RuleTranscodes} {
		b.sample("matinee_alerts_firing", float64(counts[rule]), "rule", rule)
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Write([]byte(b.String()))
}

// diskUses names what's on a disk, such as "data,transcode,library".
func diskUses(d healthDisk) string {
	var uses []string
	for _, u := range d.Uses {
		if !slices.Contains(uses, u.Use) {
			uses = append(uses, u.Use)
		}
	}
	sort.Strings(uses)
	return strings.Join(uses, ",")
}

// metricsWriter writes Prometheus's text format.
type metricsWriter struct {
	strings.Builder
}

func (b *metricsWriter) help(name, kind, help string) {
	fmt.Fprintf(b, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, kind)
}

func (b *metricsWriter) sample(name string, v float64, labels ...string) {
	b.WriteString(name)
	if len(labels) > 0 {
		b.WriteByte('{')
		for i := 0; i+1 < len(labels); i += 2 {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(labels[i] + `="` + labelValue(labels[i+1]) + `"`)
		}
		b.WriteByte('}')
	}
	b.WriteString(" " + strconv.FormatFloat(v, 'f', -1, 64) + "\n")
}

func (b *metricsWriter) gauge(name, help string, v float64, labels ...string) {
	b.help(name, "gauge", help)
	b.sample(name, v, labels...)
}

// histogram writes h in Prometheus's histogram format.
func (b *metricsWriter) histogram(name, help string, h *stream.Histogram) {
	bounds, cum, sum := h.Snapshot()
	b.help(name, "histogram", help)
	for i, le := range bounds {
		b.sample(name+"_bucket", float64(cum[i]), "le", strconv.FormatFloat(le, 'f', -1, 64))
	}
	b.sample(name+"_bucket", float64(cum[len(cum)-1]), "le", "+Inf")
	b.sample(name+"_sum", sum)
	b.sample(name+"_count", float64(cum[len(cum)-1]))
}

func (b *metricsWriter) counter(name, help string, v float64) {
	b.help(name, "counter", help)
	b.sample(name, v)
}

var labelEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)

func labelValue(s string) string { return labelEscaper.Replace(s) }
