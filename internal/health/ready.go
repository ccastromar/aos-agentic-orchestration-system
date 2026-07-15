package health

import (
	"net/http"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/runtime"
)

func ReadyHandler(rt *runtime.Runtime) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {

		if !rt.SpecsLoaded {
			http.Error(w, "specs not loaded", http.StatusServiceUnavailable)
			return
		}

  if err := rt.LLMClient.Ping(r.Context()); err != nil {
            http.Error(w, "llm unreachable", http.StatusServiceUnavailable)
            return
        }

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ready"}`))
	}
}
