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
