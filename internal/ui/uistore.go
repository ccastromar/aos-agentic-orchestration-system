package ui

import (
	"encoding/json"
	"html/template"
	"net/http"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

/*
	Event model
*/

type Event struct {
	Time     time.Time
	Agent    string
	Kind     string
	Message  string
	Duration string
}

/*
	UIStore
	- Read model de tareas
	- Estado de UI (errores de dominio)
*/

type UIStore struct {
	mu sync.RWMutex

	tasks map[string][]Event

	// injected domain dispatcher
	dispatcher AskDispatcher

	// UI state
	ErrorCode    string
	ErrorMessage string
}

type AskDispatcher interface {
	DispatchAskInternal(message string, lang string) (taskID string, err error)
}

// NewUIStore creates a UIStore. The dispatcher parameter is optional.
// It can be called as NewUIStore() or NewUIStore(dispatcher).
func NewUIStore(dispatcher ...AskDispatcher) *UIStore {
    var d AskDispatcher
    if len(dispatcher) > 0 {
        d = dispatcher[0]
    }
    return &UIStore{
        tasks:      make(map[string][]Event),
        dispatcher: d,
    }
}

/*
	UI state helpers
*/

func (s *UIStore) setError(code, msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ErrorCode = code
	s.ErrorMessage = msg
}

func (s *UIStore) clearError() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ErrorCode = ""
	s.ErrorMessage = ""
}

func (s *UIStore) SetDispatcher(d AskDispatcher) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dispatcher = d
}

/*
	Event handling
*/

// AddEvent registers an event
func (s *UIStore) AddEvent(taskID, agent, kind, msg, duration string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ev := Event{
		Time:     time.Now(),
		Agent:    agent,
		Kind:     kind,
		Message:  msg,
		Duration: duration,
	}
	s.tasks[taskID] = append(s.tasks[taskID], ev)
}

// snapshot returns a safe copy of the data
func (s *UIStore) snapshot() map[string][]Event {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make(map[string][]Event, len(s.tasks))
	for k, v := range s.tasks {
		cp := make([]Event, len(v))
		copy(cp, v)
		out[k] = cp
	}
	return out
}

/*
	UI handlers
*/

// HandleIndex shows task list
func (s *UIStore) HandleIndex(w http.ResponseWriter, r *http.Request) {
	data := s.snapshot()

	type row struct {
		ID        string
		LastEvent Event
		Count     int
	}

	rows := make([]row, 0, len(data))
	for id, evs := range data {
		if len(evs) == 0 {
			continue
		}
		rows = append(rows, row{
			ID:        id,
			LastEvent: evs[len(evs)-1],
			Count:     len(evs),
		})
	}

	sort.Slice(rows, func(i, j int) bool {
		return rows[i].LastEvent.Time.After(rows[j].LastEvent.Time)
	})

	tpl := template.Must(template.ParseFiles(
		filepath.Join("templates", "ui", "index.html"),
	))

	if err := tpl.Execute(w, rows); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// HandleTask shows full timeline of a task
func (s *UIStore) HandleTask(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Redirect(w, r, "/ui", http.StatusFound)
		return
	}

	data := s.snapshot()
	events, ok := data[id]
	if !ok {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}

	sort.Slice(events, func(i, j int) bool {
		return events[i].Time.Before(events[j].Time)
	})

	tpl := template.Must(template.ParseFiles(
		filepath.Join("templates", "ui", "task.html"),
	))

	if err := tpl.Execute(w, struct {
		ID     string
		Events []Event
	}{
		ID:     id,
		Events: events,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

/*
	/ui/ask
	- GET  -> render form
	- POST -> dispatch ask, handle intent errors
*/

func (s *UIStore) HandleAsk(w http.ResponseWriter, r *http.Request) {
	switch r.Method {

	case http.MethodGet:
		s.renderAsk(w)
		return

	case http.MethodPost:
		s.handleAskPost(w, r)
		return

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *UIStore) renderAsk(w http.ResponseWriter) {
	s.mu.RLock()
	data := struct {
		ErrorMessage string
	}{
		ErrorMessage: s.ErrorMessage,
	}
	s.mu.RUnlock()

	tpl := template.Must(template.ParseFiles(
		filepath.Join("templates", "ui", "ask.html"),
	))

	if err := tpl.Execute(w, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *UIStore) handleAskPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.setError("invalid_payload", "Invalid form submission")
		http.Redirect(w, r, "/ui/ask", http.StatusFound)
		return
	}
	lang := r.FormValue("lang")
	if lang == "" {
		lang = "en" // default explícito
	}

	message := r.FormValue("message")
	allowed := map[string]bool{"en": true, "es": true, "fr": true, "de": true}
	if !allowed[lang] {
		s.setError("invalid_lang", "Unsupported language")
		http.Redirect(w, r, "/ui/ask", http.StatusFound)
		return
	}

	if message == "" {
		s.setError("empty_message", "Message cannot be empty")
		http.Redirect(w, r, "/ui/ask", http.StatusFound)
		return
	}

	if s.dispatcher == nil {
		s.setError("internal_error", "Dispatcher not configured")
		http.Redirect(w, r, "/ui/ask", http.StatusFound)
		return
	}

	taskID, err := s.dispatcher.DispatchAskInternal(message, lang)
	if err != nil {
		s.setError("invalid_intent", err.Error())
		http.Redirect(w, r, "/ui/ask", http.StatusFound)
		return
	}

	s.clearError()
	http.Redirect(w, r, "/ui/task?id="+taskID, http.StatusFound)
}

func (s *UIStore) HandleTaskEvents(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "missing task id", http.StatusBadRequest)
		return
	}

	s.mu.RLock()
	events, ok := s.tasks[id]
	s.mu.RUnlock()

	if !ok {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(events)
}
