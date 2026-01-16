package helpdesk

import (
	"encoding/json"
	"net/http"
)

func RegisterHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/mock/support/ticket", manageTicker)
	mux.HandleFunc("/mock/support/ticket/note", ticketNote)
	mux.HandleFunc("/mock/support/ticket/close", ticketClose)
}

// Crear ticket
func manageTicker(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		handleCreateTicket(w, r)
		return
	}
	if r.Method == "GET" {
		handleGetTicket(w, r)
		return
	}

	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

// Añadir nota
func ticketNote(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		handleAddNote(w, r)
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

// Cerrar ticket
func ticketClose(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		handleCloseTicket(w, r)
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

func handleCreateTicket(w http.ResponseWriter, r *http.Request) {
	var body map[string]string
	_ = json.NewDecoder(r.Body).Decode(&body)

	resp := map[string]any{
		"ticketId":    "TCK-" + randomId(),
		"status":      "open",
		"subject":     body["subject"],
		"description": body["description"],
		"priority":    body["priority"],
	}

	json.NewEncoder(w).Encode(resp)
}

func handleGetTicket(w http.ResponseWriter, r *http.Request) {
	ticketId := r.URL.Query().Get("ticketId")

	resp := map[string]any{
		"ticketId": ticketId,
		"status":   "open",
		"subject":  "Sample issue",
		"history": []map[string]any{
			{"date": "2025-01-10", "note": "Customer reported issue"},
			{"date": "2025-01-11", "note": "Agent requested more info"},
		},
	}

	json.NewEncoder(w).Encode(resp)
}

func handleAddNote(w http.ResponseWriter, r *http.Request) {
	var body map[string]string
	_ = json.NewDecoder(r.Body).Decode(&body)

	resp := map[string]any{
		"ticketId": body["ticketId"],
		"added":    true,
		"note":     body["note"],
	}

	json.NewEncoder(w).Encode(resp)
}

func handleCloseTicket(w http.ResponseWriter, r *http.Request) {
	var body map[string]string
	_ = json.NewDecoder(r.Body).Decode(&body)

	resp := map[string]any{
		"ticketId": body["ticketId"],
		"closed":   true,
		"reason":   body["reason"],
	}

	json.NewEncoder(w).Encode(resp)
}

// ----------------------------
// Utils
// ----------------------------
func randomId() string {
	return RandString(6)
}

const letters = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func RandString(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[i%len(letters)]
	}
	return string(b)
}
