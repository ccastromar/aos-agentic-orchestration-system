package openapi

import (
	"encoding/json"
	"net/http"
	"strings"
)

func RegisterHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/mock/openapi/users/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":     strings.TrimPrefix(r.URL.Path, "/mock/openapi/users/"),
			"name":   "OpenAPI User",
			"status": "active",
		})
	})

	mux.HandleFunc("/mock/openapi/posts", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		resp, _ := json.Marshal(map[string]any{
			"id":           "post-999",
			"status":       "created",
			"receivedData": body,
		})
		w.Write(resp)
	})
}
