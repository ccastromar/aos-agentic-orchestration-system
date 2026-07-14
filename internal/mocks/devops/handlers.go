package devops

import (
	"encoding/json"
	"math/rand"
	"net/http"
	"strconv"
	"time"
)

func RegisterHandlers(mux *http.ServeMux) {

	mux.HandleFunc("/mock/devops/status", handleStatus)
	mux.HandleFunc("/mock/devops/restart", handleRestart)
	mux.HandleFunc("/mock/devops/deploy", handleDeploy)
	mux.HandleFunc("/mock/devops/logs", handleLogs)

}

//
// /devops/status
//

type StatusResponse struct {
	Service   string `json:"service"`
	Status    string `json:"status"`
	Uptime    string `json:"uptime"`
	Version   string `json:"version"`
	Instances int    `json:"instances"`
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	service := r.URL.Query().Get("service")
	if service == "" {
		service = "unknown"
	}

	response := map[string]any{
		"service":   service,
		"status":    "running",
		"instances": 3,
		"uptime":    "32h",
		"version":   "1.4.2",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

//
// /devops/restart
//

type RestartResponse struct {
	Service string `json:"service"`
	Result  string `json:"result"`
	Message string `json:"message"`
	Took    string `json:"took"`
}

func handleRestart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Service string `json:"service"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// fallback to form parsing just in case
		r.ParseForm()
		req.Service = r.Form.Get("service")
	}

	service := req.Service
	if service == "" {
		service = "unknown-service"
	}

	// Simulamos tiempo de reinicio
	time.Sleep(time.Duration(500+rand.Intn(300)) * time.Millisecond)

	resp := RestartResponse{
		Service: service,
		Result:  "ok",
		Message: "service restarted successfully",
		Took:    "750ms",
	}

	jsonResponse(w, resp)
}

//
// /devops/deploy
//

type DeployResponse struct {
	Service string `json:"service"`
	Version string `json:"version"`
	Status  string `json:"status"`
	Message string `json:"message"`
	Took    string `json:"took"`
}

func handleDeploy(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Service string `json:"service"`
		Version string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		r.ParseForm()
		req.Service = r.Form.Get("service")
		req.Version = r.Form.Get("version")
	}

	service := req.Service
	version := req.Version
	if service == "" {
		service = "unknown-service"
	}
	if version == "" {
		version = "latest"
	}

	time.Sleep(time.Duration(600+rand.Intn(400)) * time.Millisecond)

	resp := DeployResponse{
		Service: service,
		Version: version,
		Status:  "success",
		Message: "deployment completed without issues",
		Took:    "1.1s",
	}

	jsonResponse(w, resp)
}

//
// /devops/logs
//

type LogResponse struct {
	Service string   `json:"service"`
	Lines   int      `json:"lines"`
	Logs    []string `json:"logs"`
}

func handleLogs(w http.ResponseWriter, r *http.Request) {
	service := r.URL.Query().Get("service")
	l := r.URL.Query().Get("lines")
	if service == "" {
		service = "unknown-service"
	}

	lines, _ := strconv.Atoi(l)
	if lines <= 0 {
		lines = 10
	}

	logs := make([]string, lines)
	for i := 0; i < lines; i++ {
		logs[i] = time.Now().Format(time.RFC3339) + " [" + service + "] sample log line " + strconv.Itoa(i+1)
	}

	resp := LogResponse{
		Service: service,
		Lines:   lines,
		Logs:    logs,
	}

	jsonResponse(w, resp)
}

//
// helpers
//

func jsonResponse(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(v)
}
