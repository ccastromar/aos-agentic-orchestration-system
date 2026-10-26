package agent

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/metrics"
)

func runCount(t *testing.T, outcome string) float64 {
	t.Helper()
	body := dumpMetrics()
	needle := `aos_run_duration_seconds_count{outcome="` + outcome + `"} `
	i := strings.Index(body, needle)
	if i < 0 {
		return 0
	}
	var n float64
	_, _ = fmt.Sscan(body[i+len(needle):], &n)
	return n
}

// A run is recorded once, when its result is final; a wait for a person stops the clock.
func TestARunIsRecordedOnceWithItsOutcome(t *testing.T) {
	before := runCount(t, "blocked")
	markRunStart("run-1")
	storeResult("run-1", Result{Status: "governance_blocked"})
	storeResult("run-1", Result{Status: "governance_blocked"})
	if got := runCount(t, "blocked") - before; got != 1 {
		t.Fatalf("blocked runs recorded = %v, want 1", got)
	}

	completed := runCount(t, "completed")
	markRunStart("run-2")
	storeResult("run-2", Result{Status: "await_human"})
	storeResult("run-2", Result{Status: "completed"}) // no resume marked: not a run of its own
	if got := runCount(t, "completed") - completed; got != 0 {
		t.Fatalf("a run that waited was recorded without its resume: %v", got)
	}
	markRunStart("run-2")
	storeResult("run-2", Result{Status: "completed"})
	if got := runCount(t, "completed") - completed; got != 1 {
		t.Fatalf("completed runs recorded = %v, want 1", got)
	}
}

func dumpMetrics() string {
	rec := httptest.NewRecorder()
	metrics.ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	return rec.Body.String()
}
