package metrics

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// A histogram is exported as cumulative buckets, so an indicator ("decisions within 0.25 s")
// is one bucket over the count.
func TestHistogramsAreCumulativeBuckets(t *testing.T) {
	h := NewHistogramVec("x_seconds", "x", []float64{0.1, 0.25, 1}, "verdict")
	for _, v := range []float64{0.05, 0.2, 0.2, 3} {
		h.Observe(map[string]string{"verdict": "ALLOW"}, v)
	}
	labels := map[string]string{"verdict": "ALLOW"}
	counts := h.counts[makeKey(labels)]
	if want := []float64{1, 3, 3, 4}; len(counts) != 4 || counts[0] != want[0] || counts[1] != want[1] || counts[2] != want[2] || counts[3] != want[3] {
		t.Fatalf("counts = %v, want %v", counts, want)
	}
}

func TestDomainMetricsAreExposed(t *testing.T) {
	GateDuration.Observe(map[string]string{"verdict": "DENY"}, 0.03)
	EvidenceShipped.Add(map[string]string{"transport": "kafka", "outcome": "success"}, 3)
	EvidenceBacklog.Set(map[string]string{"transport": "kafka"}, 512)

	rec := httptest.NewRecorder()
	ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	body := rec.Body.String()
	for _, want := range []string{
		"# TYPE aos_gate_duration_seconds histogram",
		`aos_gate_duration_seconds_bucket{verdict="DENY",le="0.05"} 1`,
		`aos_gate_duration_seconds_bucket{verdict="DENY",le="+Inf"} 1`,
		`aos_gate_duration_seconds_count{verdict="DENY"} 1`,
		`aos_evidence_shipped_total{outcome="success",transport="kafka"} 3`,
		`aos_evidence_backlog_bytes{transport="kafka"} 512`,
		"# TYPE aos_run_duration_seconds histogram",
		"# TYPE aos_step_duration_seconds histogram",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
}

// Asked for OpenMetrics (as Prometheus does), a bucket carries the trace of its latest
// measurement: the way from the histogram to the trace. The classic format has no exemplars.
func TestOpenMetricsCarriesExemplars(t *testing.T) {
	h := NewHistogramVec("x_seconds", "x", []float64{0.1, 0.25}, "verdict")
	h.ObserveWithTrace(map[string]string{"verdict": "ALLOW"}, 0.2, "0af7651916cd43dd8448eb211c80319c")
	GateDuration.ObserveWithTrace(map[string]string{"verdict": "ALLOW"}, 0.2, "0af7651916cd43dd8448eb211c80319c")

	req := httptest.NewRequest("GET", "/metrics", nil)
	req.Header.Set("Accept", "application/openmetrics-text;version=1.0.0,text/plain;version=0.0.4;q=0.5")
	rec := httptest.NewRecorder()
	ServeHTTP(rec, req)
	body := rec.Body.String()
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/openmetrics-text") {
		t.Fatalf("content type %q", ct)
	}
	if !strings.Contains(body, `aos_gate_duration_seconds_bucket{verdict="ALLOW",le="0.25"} `) ||
		!strings.Contains(body, `# {trace_id="0af7651916cd43dd8448eb211c80319c"} 0.2 `) {
		t.Errorf("no exemplar on the 0.25 bucket:\n%s", body)
	}
	if !strings.Contains(body, "# TYPE aos_http_requests counter") || !strings.HasSuffix(body, "# EOF\n") {
		t.Error("not OpenMetrics: counter family or # EOF")
	}

	rec = httptest.NewRecorder()
	ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	if strings.Contains(rec.Body.String(), "trace_id") || strings.Contains(rec.Body.String(), "# EOF") {
		t.Error("the classic format must not carry exemplars")
	}
}

// The runtime and process metrics are there, under Prometheus's usual names, in both formats.
func TestRuntimeMetricsAreExposed(t *testing.T) {
	for _, accept := range []string{"", "application/openmetrics-text"} {
		req := httptest.NewRequest("GET", "/metrics", nil)
		req.Header.Set("Accept", accept)
		rec := httptest.NewRecorder()
		ServeHTTP(rec, req)
		body := rec.Body.String()
		for _, want := range []string{"go_goroutines ", "go_memstats_heap_alloc_bytes ", "go_gc_cycles_total ", "process_cpu_seconds_total ", "process_start_time_seconds "} {
			if !strings.Contains(body, want) {
				t.Errorf("Accept %q: missing %s", accept, want)
			}
		}
	}
}
