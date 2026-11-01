package health

import (
	"encoding/json"
	"net/http"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/runtime"
)

func ReadyHandler(rt *runtime.Runtime) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {

		if !rt.SpecsLoaded {
			http.Error(w, "specs not loaded", http.StatusServiceUnavailable)
			return
		}

		// Deterministic mode has no LLM client to depend on.
		if rt.LLMClient != nil {
			if err := rt.LLMClient.Ping(r.Context()); err != nil {
				http.Error(w, "llm unreachable", http.StatusServiceUnavailable)
				return
			}
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":     "ready",
			"execution":  rt.Execution,
			"governance": rt.Governance,
			"evidence":   rt.Evidence,
		})
	}
}
