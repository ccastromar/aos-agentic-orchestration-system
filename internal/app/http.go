package app

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
	"strings"

	"time"

	"strconv"

	"github.com/ccastromar/aos-agent-orchestration-system/internal/agent"
	"github.com/ccastromar/aos-agent-orchestration-system/internal/health"
	"github.com/ccastromar/aos-agent-orchestration-system/internal/logx"
	"github.com/ccastromar/aos-agent-orchestration-system/internal/metrics"
	"github.com/ccastromar/aos-agent-orchestration-system/internal/runtime"
	"github.com/ccastromar/aos-agent-orchestration-system/internal/ui"
)

type HTTPServer struct {
	srv *http.Server
}

// httpPort holds the port used by the HTTP server. Default is 9090.
var httpPort = "9090"

// SetHTTPPort allows overriding the default HTTP port before starting the app.
func SetHTTPPort(p string) {
	if p == "" {
		return
	}
	httpPort = p
}

func NewHTTPServer(apiAgent *agent.APIAgent, uiStore *ui.UIStore, rt *runtime.Runtime) *HTTPServer {
	mux := http.NewServeMux()

	apiAgent.RegisterHTTP(mux)
	mux.HandleFunc("/ui", uiStore.HandleIndex)
	mux.HandleFunc("/ui/task", uiStore.HandleTask)
	mux.HandleFunc("/ui/ask", uiStore.HandleAsk)
	mux.HandleFunc("/ui/task/events", uiStore.HandleTaskEvents)

	mux.HandleFunc("/health/live", health.LiveHandler)
	mux.HandleFunc("/health/ready", health.ReadyHandler(rt))
	// Expose Prometheus metrics
	mux.HandleFunc("/metrics", metrics.ServeHTTP)
	mux.Handle("/static/",
		http.StripPrefix("/static/",
			http.FileServer(http.Dir("web/static")),
		),
	)

	// Wrap with security and observability middleware
	hardened := secureMiddleware(observabilityMiddleware(mux))

	return &HTTPServer{
		srv: &http.Server{
			Addr:              ":" + httpPort,
			Handler:           hardened,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       10 * time.Second,
			WriteTimeout:      15 * time.Second,
			IdleTimeout:       60 * time.Second,
			MaxHeaderBytes:    1 << 20, // 1MB
		},
	}
}

func (h *HTTPServer) Start(ctx context.Context) error {
	errCh := make(chan error, 1)

	go func() {
		logx.Info("HTTP", "listening on port :%s", httpPort)
		errCh <- h.srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		logx.Info("HTTP", "shutting down server...")
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return h.srv.Shutdown(shutCtx)
	}
}

// secureMiddleware adds basic hardening to HTTP server:
// - Common security headers
// - Body size limit
// - Block TRACE method
func secureMiddleware(next http.Handler) http.Handler {
	const maxBody = 1 << 20 // 1MB
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block TRACE to avoid request smuggling tricks
		if r.Method == http.MethodTrace {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		// Limit body size early
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		}

		// Security headers
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		// Modern browsers ignore X-XSS-Protection; set to 0 to disable legacy filter quirks
		w.Header().Set("X-XSS-Protection", "0")
		// A conservative CSP that should not break our minimal UI
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'")
		// HSTS only when TLS is enabled
		if r.TLS != nil {
			w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains; preload")
		}

		next.ServeHTTP(w, r)
	})
}

// observabilityMiddleware adds minimal structured request logging and propagates IDs.
// It ensures each request has a request ID and optional correlation ID, exposes them
// in response headers, and logs a single JSON line with basic request metrics.
func observabilityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID, corrID := requestAndCorrelationID(r)

		// Expose IDs in response headers
		w.Header().Set("X-Request-Id", reqID)
		if corrID != "" {
			w.Header().Set("X-Correlation-Id", corrID)
		}

		// Wrap ResponseWriter to capture status and size
		rw := &respWriter{ResponseWriter: w, status: http.StatusOK}

		start := time.Now()
		next.ServeHTTP(rw, r)
		dur := time.Since(start)

		// Record basic HTTP metrics
		labels := map[string]string{
			"method": r.Method,
			"path":   r.URL.Path,
			"status": strconv.Itoa(rw.status),
		}
		metrics.HTTPRequests.Inc(labels)
		metrics.HTTPDuration.Observe(labels, float64(dur)/float64(time.Second))

		// Structured log line
		// Keep it minimal and dependency-free
		logx.G("HTTP",
			`{"ts":"%s","level":"info","component":"http","request_id":"%s","correlation_id":"%s","method":"%s","path":"%s","status":%d,"duration_ms":%d,"size":%d,"remote":"%s","ua":"%s"}`,
			time.Now().Format(time.RFC3339),
			reqID,
			corrID,
			r.Method,
			r.URL.Path,
			rw.status,
			dur.Milliseconds(),
			rw.size,
			remoteHost(r.RemoteAddr),
			sanitizeUserAgent(r.UserAgent()),
		)
	})
}

// respWriter captures HTTP status code and response size.
type respWriter struct {
	http.ResponseWriter
	status int
	size   int
}

func (w *respWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *respWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.size += n
	return n, err
}

// requestAndCorrelationID extracts or generates IDs from headers.
func requestAndCorrelationID(r *http.Request) (string, string) {
	reqID := r.Header.Get("X-Request-Id")
	if reqID == "" {
		reqID = genReqID()
	}
	// Accept both common header spellings
	corrID := r.Header.Get("X-Correlation-Id")
	if corrID == "" {
		corrID = r.Header.Get("X-Correlation-ID")
	}
	return reqID, corrID
}

// genReqID returns a 16-byte hex-encoded random ID, or a timestamp fallback.
func genReqID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err == nil {
		const hexdigits = "0123456789abcdef"
		dst := make([]byte, 32)
		for i, v := range b {
			dst[i*2] = hexdigits[v>>4]
			dst[i*2+1] = hexdigits[v&0x0f]
		}
		return string(dst)
	}
	return fmt.Sprintf("ts-%d", time.Now().UnixNano())
}

// remoteHost strips the port from host:port
func remoteHost(addr string) string {
	if i := strings.LastIndex(addr, ":"); i > 0 {
		return addr[:i]
	}
	return addr
}

// sanitizeUserAgent trims and bounds the UA string length for logs.
func sanitizeUserAgent(ua string) string {
	ua = strings.TrimSpace(ua)
	if len(ua) > 200 {
		return ua[:200]
	}
	return ua
}
