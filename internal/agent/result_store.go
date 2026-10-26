package agent

import (
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/metrics"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/telemetry"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Result struct {
	Status string      `json:"status"`
	Data   interface{} `json:"data,omitempty"`
	Err    string      `json:"error,omitempty"`
}

var (
	resultsMu sync.Mutex
	results   = make(map[string]Result)
)

func storeResult(id string, res Result) {
	observeRun(id, res.Status)
	resultsMu.Lock()
	defer resultsMu.Unlock()
	results[id] = res
}

// runStarts holds when each run started, or resumed after waiting for a person or a callback.
var runStarts sync.Map

// markRunStart starts the clock of run id's aos_run_duration_seconds.
func markRunStart(id string) {
	runStarts.Store(id, time.Now())
}

// runOutcome is the outcome label of a final status, or "" for one that is not final.
var runOutcome = map[string]string{
	"completed":          "completed",
	"ok":                 "completed",
	"governance_blocked": "blocked",
	"error":              "error",
	"timeout":            "timeout",
}

// observeRun records a run when its result is final. A wait for a person or a callback stops
// the clock without recording: that time is theirs, and the resume starts it again.
func observeRun(id, status string) {
	if strings.HasPrefix(status, "await_") {
		runStarts.Delete(id)
		return
	}
	outcome, final := runOutcome[status]
	if !final {
		return
	}
	if started, ok := runStarts.LoadAndDelete(id); ok {
		traceID := ""
		if taskCtx, ok := GetTaskContext(id); ok {
			traceID = telemetry.SampledTraceID(taskCtx)
		}
		metrics.RunDuration.ObserveWithTrace(map[string]string{"outcome": outcome}, time.Since(started.(time.Time)).Seconds(), traceID)
	}
}

// getResult retrieves a stored result by id.
// The second return value indicates whether a result was found.
func getResult(id string) (Result, bool) {
	resultsMu.Lock()
	defer resultsMu.Unlock()
	r, ok := results[id]
	return r, ok
}

// deleteResult removes a result from the store to avoid unbounded growth.
func deleteResult(id string) {
	resultsMu.Lock()
	defer resultsMu.Unlock()
	delete(results, id)
}

func waitForResult(id string, timeout time.Duration) Result {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
		resultsMu.Lock()
		r, ok := results[id]
		resultsMu.Unlock()
		if ok {
			return r
		}
	}
	return Result{
		Status: "timeout",
		Err:    "timeout waiting for a result",
	}
}

func randomID() string {
	return uuid.NewString()
}
