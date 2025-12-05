package ui

import (
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type Event struct {
	Time     time.Time
	Agent    string
	Kind     string
	Message  string
	Duration string
}

type UIStore struct {
	mu    sync.RWMutex
	tasks map[string][]Event
}

func NewUIStore() *UIStore {
	return &UIStore{
		tasks: make(map[string][]Event),
	}
}

// AddEvent register an event
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

// snapshot get a copy of the data
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

// HandleIndex muestra la lista de tareas y el último evento por cada una.
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
		return
	}
}

// HandleTask muestra el timeline completo de una tarea.
func (s *UIStore) HandleTask(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Redirect(w, r, "/ui", http.StatusFound)
		return
	}

	data := s.snapshot()
	events, ok := data[id]
	if !ok {
		http.Error(w, "task no encontrada", http.StatusNotFound)
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
		return
	}
}

func (u *UIStore) HandleAsk(w http.ResponseWriter, r *http.Request) {
	//tmpl, err := template.ParseFiles("ask.html")
	tmpl := template.Must(template.ParseFiles(
		filepath.Join("templates", "ui", "ask.html"),
	))

	data := struct {
		APIKey string
	}{
		APIKey: os.Getenv("API_KEY"),
	}

	_ = tmpl.Execute(w, data)
}
