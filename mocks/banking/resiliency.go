package banking

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// The resiliency demo (pipeline_resiliency_demo) behaves like test/e2e/resiliency_test.go: the
// flaky step succeeds on its third attempt, inside its two retries, and the slow step outlasts its
// 100 ms timeout, so the run ends in a timeout error by design.

const flakyFailures = 2

var flaky = struct {
	sync.Mutex
	failures map[string]int
}{failures: map[string]int{}}

// postFlaky fails the first flakyFailures calls for each param1 and then succeeds once, so every
// run of the demo starts the cycle again.
func postFlaky(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Param1 string `json:"param1"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	flaky.Lock()
	failed := flaky.failures[body.Param1]
	if failed < flakyFailures {
		flaky.failures[body.Param1] = failed + 1
	} else {
		delete(flaky.failures, body.Param1)
	}
	flaky.Unlock()

	if failed < flakyFailures {
		http.Error(w, "temporary failure", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "attempt": failed + 1})
}

func postSlow(w http.ResponseWriter, r *http.Request) {
	select {
	case <-time.After(300 * time.Millisecond):
	case <-r.Context().Done():
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
}
