package agent

import (
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
	resultsMu.Lock()
	defer resultsMu.Unlock()
	results[id] = res
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
