package logx

import (
	"context"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/trace"
)

const (
	Reset = "\033[0m"

	Red     = "\033[31m"
	Green   = "\033[32m"
	Yellow  = "\033[33m"
	Blue    = "\033[34m"
	Magenta = "\033[35m"
	Cyan    = "\033[36m"
)

// colors
var levelColor = map[string]string{
	"DEBUG": Cyan,
	"INFO":  Blue,
	"WARN":  Yellow,
	"ERROR": Red,
}

// colors by agent
var agentColor = map[string]string{
	"Api":       Cyan,
	"Planner":   Blue,
	"Inspector": Magenta,
	"Verifier":  Green,
	"Analyst":   Yellow,
	"Mock":      Cyan,
	"HTTP":      Blue,
	"Config":    Magenta,
	"App":       Green,
}

// detect color mode
func useColor() bool {
	return os.Getenv("LOG_COLOR") == "1"
}

// --- Configuration ---

// Config is how AOS logs (Chestertown observability conventions, section 5).
type Config struct {
	// Format is "json" (one object per line on stdout) or "text"; empty means json, except in
	// development.
	Format string
	// Level is the lowest severity written: debug, info (default), warn or error. Debug logs
	// may hold what users type: for local development only.
	Level       string
	Development bool
	Service     string
	Version     string
	Environment string
}

var (
	mu       sync.RWMutex
	minLevel = slog.LevelDebug
	jsonOut  *slog.Logger
	// taskContext finds a task's context, for the trace of the lines logged about it.
	taskContext func(id string) (context.Context, bool)
)

// Configure applies cfg. Until it is called (tests, tools) AOS logs text at every level.
func Configure(cfg Config) {
	mu.Lock()
	defer mu.Unlock()
	minLevel = parseLevel(cfg.Level)
	format := strings.ToLower(strings.TrimSpace(cfg.Format))
	if format == "" && !cfg.Development {
		format = "json"
	}
	if format != "json" {
		jsonOut = nil
		return
	}
	jsonOut = NewJSONLogger(os.Stdout, cfg)
	// The standard library's log package (and anything that uses it) writes through it too.
	slog.SetDefault(jsonOut)
}

// NewJSONLogger is a logger writing the suite's JSON lines to w.
func NewJSONLogger(w io.Writer, cfg Config) *slog.Logger {
	service := cfg.Service
	if service == "" {
		service = "aos"
	}
	version := cfg.Version
	if version == "" {
		version = "dev"
	}
	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: slog.LevelDebug,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) > 0 {
				return a
			}
			switch a.Key {
			case slog.TimeKey:
				return slog.String("timestamp", a.Value.Time().UTC().Format("2006-01-02T15:04:05.000Z07:00"))
			case slog.LevelKey:
				return slog.String("severity", severity(a.Value.Any().(slog.Level)))
			case slog.MessageKey:
				return slog.String("message", a.Value.String())
			}
			return a
		},
	})
	return slog.New(handler).With(
		"service.name", service,
		"service.version", version,
		"deployment.environment.name", cfg.Environment,
	)
}

// SetTaskContext lets the lines logged about a task (L) carry its trace.
func SetTaskContext(lookup func(id string) (context.Context, bool)) {
	mu.Lock()
	taskContext = lookup
	mu.Unlock()
}

func parseLevel(value string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func severity(level slog.Level) string {
	switch {
	case level >= slog.LevelError:
		return "ERROR"
	case level >= slog.LevelWarn:
		return "WARN"
	case level >= slog.LevelInfo:
		return "INFO"
	default:
		return "DEBUG"
	}
}

func current() (slog.Level, *slog.Logger) {
	mu.RLock()
	defer mu.RUnlock()
	return minLevel, jsonOut
}

// --- Public API ---

func Debug(agent, msg string, args ...any) {
	logGeneric(slog.LevelDebug, agent, msg, args...)
}

func Info(agent, msg string, args ...any) {
	logGeneric(slog.LevelInfo, agent, msg, args...)
}

func Warn(agent, msg string, args ...any) {
	logGeneric(slog.LevelWarn, agent, msg, args...)
}

func Error(agent, msg string, args ...any) {
	logGeneric(slog.LevelError, agent, msg, args...)
}

// Event logs a named event (`event.name`) with attributes (key, value pairs: `outcome`,
// `error.type`, domain ids), carrying ctx's trace and the correlation id ctx was given with
// attrs. Attributes are ids and fixed values, never what a user typed.
func Event(ctx context.Context, level slog.Level, agent, name, msg string, attrs ...any) {
	min, out := current()
	if level < min {
		return
	}
	attrs = append([]any{"component", agent, "event.name", name}, attrs...)
	attrs = append(attrs, traceAttrs(ctx)...)
	if out != nil {
		out.Log(context.Background(), level, msg, attrs...)
		return
	}
	log.Printf("[%s] [%s] %s %s", severity(level), agent, msg, textAttrs(attrs[2:]))
}

// --- Core ---

func logGeneric(level slog.Level, agent, msg string, args ...any) {
	min, out := current()
	if level < min {
		return
	}
	full := fmt.Sprintf(msg, args...)
	if out != nil {
		out.Log(context.Background(), level, full, "component", agent)
		return
	}
	name := severity(level)
	if useColor() {
		lc := levelColor[name]
		ac := agentColor[agent]
		log.Printf("%s[%s]%s %s[%s]%s %s",
			lc, name, Reset,
			ac, agent, Reset,
			full,
		)
	} else {
		log.Printf("[%s] [%s] %s", name, agent, full)
	}
}

// L logs about task id: in JSON, the id is the line's correlation_id and the task's trace its
// trace_id.
func L(id, agent, msg string, args ...any) {
	min, out := current()
	if slog.LevelInfo < min {
		return
	}
	if out != nil {
		attrs := []any{"component", agent, "correlation_id", id}
		mu.RLock()
		lookup := taskContext
		mu.RUnlock()
		if lookup != nil {
			if ctx, ok := lookup(id); ok && ctx != nil {
				attrs = append(attrs, traceAttrs(ctx)...)
			}
		}
		out.Log(context.Background(), slog.LevelInfo, fmt.Sprintf(msg, args...), attrs...)
		return
	}
	prefix := fmt.Sprintf("[%s][%s][%s] ",
		time.Now().Format(time.RFC3339),
		agent,
		id,
	)
	log.Printf(prefix+msg, args...)
}

func G(agent, msg string, args ...any) {
	min, out := current()
	if slog.LevelInfo < min {
		return
	}
	if out != nil {
		out.Log(context.Background(), slog.LevelInfo, fmt.Sprintf(msg, args...), "component", agent)
		return
	}
	prefix := fmt.Sprintf("[%s][%s] ",
		time.Now().Format(time.RFC3339),
		agent,
	)
	log.Printf(prefix+msg, args...)
}

func traceAttrs(ctx context.Context) []any {
	if ctx == nil {
		return nil
	}
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return nil
	}
	return []any{"trace_id", sc.TraceID().String(), "span_id", sc.SpanID().String()}
}

func textAttrs(attrs []any) string {
	var b strings.Builder
	for i := 0; i+1 < len(attrs); i += 2 {
		if i > 0 {
			b.WriteByte(' ')
		}
		fmt.Fprintf(&b, "%v=%v", attrs[i], attrs[i+1])
	}
	return b.String()
}

// Keys returns a map's keys, sorted: what to log about parameters instead of their values, which
// are the user's input. Logs reach whoever operates the platform and outlive the request; values
// stay in the audit journal, where they belong.
func Keys(m any) []string {
	v := reflect.ValueOf(m)
	if v.Kind() != reflect.Map {
		return nil
	}
	keys := make([]string, 0, v.Len())
	for _, k := range v.MapKeys() {
		keys = append(keys, fmt.Sprint(k.Interface()))
	}
	sort.Strings(keys)
	return keys
}
