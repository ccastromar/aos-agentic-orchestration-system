package metrics

import (
    "fmt"
    "net/http"
    "sort"
    "strings"
    "sync"
    "time"
)

// A very small in-process metrics registry that exports Prometheus-like text.
// It supports counters and simple summaries (count/sum), with labeled samples.

type labelsKey string

func makeKey(lbls map[string]string) labelsKey {
    if len(lbls) == 0 {
        return labelsKey("")
    }
    keys := make([]string, 0, len(lbls))
    for k := range lbls { keys = append(keys, k) }
    sort.Strings(keys)
    var b strings.Builder
    for i, k := range keys {
        if i > 0 { b.WriteByte(',') }
        b.WriteString(k)
        b.WriteByte('=')
        // escape quotes
        v := strings.ReplaceAll(lbls[k], "\"", "\\\"")
        b.WriteString("\"")
        b.WriteString(v)
        b.WriteString("\"")
    }
    return labelsKey(b.String())
}

type CounterVec struct {
    Name   string
    Help   string
    mu     sync.RWMutex
    labelNames []string
    values map[labelsKey]float64
}

func NewCounterVec(name, help string, labelNames ...string) *CounterVec {
    return &CounterVec{Name: name, Help: help, labelNames: labelNames, values: make(map[labelsKey]float64)}
}

func (cv *CounterVec) Inc(lbls map[string]string) {
    cv.Add(lbls, 1)
}

func (cv *CounterVec) Add(lbls map[string]string, v float64) {
    key := makeKey(lbls)
    cv.mu.Lock()
    cv.values[key] += v
    cv.mu.Unlock()
}

// SummaryVec stores count and sum; we export metric_count and metric_sum.
type SummaryVec struct {
    Name string
    Help string
    mu   sync.RWMutex
    labelNames []string
    count map[labelsKey]float64
    sum   map[labelsKey]float64
}

func NewSummaryVec(name, help string, labelNames ...string) *SummaryVec {
    return &SummaryVec{Name: name, Help: help, labelNames: labelNames, count: make(map[labelsKey]float64), sum: make(map[labelsKey]float64)}
}

func (sv *SummaryVec) Observe(lbls map[string]string, v float64) {
    key := makeKey(lbls)
    sv.mu.Lock()
    sv.count[key] += 1
    sv.sum[key] += v
    sv.mu.Unlock()
}

// HistogramVec counts observations in cumulative buckets (upper bounds, in the metric's unit),
// exported as name_bucket{le=...}, name_sum and name_count. The bounds are chosen so the
// service-level indicators can be read from the buckets (Chestertown observability, section 8).
type HistogramVec struct {
    Name    string
    Help    string
    Buckets []float64
    mu      sync.RWMutex
    labelNames []string
    counts  map[labelsKey][]float64 // per bucket, then +Inf
    sum     map[labelsKey]float64
    // exemplars: per bucket, the trace of its latest measurement (OpenMetrics only).
    exemplars map[labelsKey][]exemplar
}

// exemplar ties a bucket to one measurement's trace: from a histogram to the trace behind it.
type exemplar struct {
    traceID string
    value   float64
    at      time.Time
}

func NewHistogramVec(name, help string, buckets []float64, labelNames ...string) *HistogramVec {
    return &HistogramVec{Name: name, Help: help, Buckets: buckets, labelNames: labelNames,
        counts: make(map[labelsKey][]float64), sum: make(map[labelsKey]float64),
        exemplars: make(map[labelsKey][]exemplar)}
}

func (hv *HistogramVec) Observe(lbls map[string]string, v float64) {
    hv.ObserveWithTrace(lbls, v, "")
}

// ObserveWithTrace is Observe that keeps traceID (a W3C trace id, hex) as the exemplar of the
// bucket v falls in, when it is not empty.
func (hv *HistogramVec) ObserveWithTrace(lbls map[string]string, v float64, traceID string) {
    key := makeKey(lbls)
    hv.mu.Lock()
    if traceID != "" {
        ex, ok := hv.exemplars[key]
        if !ok {
            ex = make([]exemplar, len(hv.Buckets)+1)
            hv.exemplars[key] = ex
        }
        i := len(hv.Buckets)
        for j, upper := range hv.Buckets {
            if v <= upper {
                i = j
                break
            }
        }
        ex[i] = exemplar{traceID: traceID, value: v, at: time.Now()}
    }
    counts, ok := hv.counts[key]
    if !ok {
        counts = make([]float64, len(hv.Buckets)+1)
        hv.counts[key] = counts
    }
    for i, upper := range hv.Buckets {
        if v <= upper {
            counts[i]++
        }
    }
    counts[len(hv.Buckets)]++
    hv.sum[key] += v
    hv.mu.Unlock()
}

// GaugeVec holds the latest value per label set.
type GaugeVec struct {
    Name   string
    Help   string
    mu     sync.RWMutex
    labelNames []string
    values map[labelsKey]float64
}

func NewGaugeVec(name, help string, labelNames ...string) *GaugeVec {
    return &GaugeVec{Name: name, Help: help, labelNames: labelNames, values: make(map[labelsKey]float64)}
}

func (gv *GaugeVec) Set(lbls map[string]string, v float64) {
    key := makeKey(lbls)
    gv.mu.Lock()
    gv.values[key] = v
    gv.mu.Unlock()
}

// Buckets (seconds) of the domain histograms.
var (
    // A decision: Prudence answers within the gate's 5 s timeout.
    GateBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5}
    // One tool call.
    StepBuckets = []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}
    // A governed run, from the request to its result (time waiting for a person excluded).
    RunBuckets = []float64{0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300, 600}
)

// Global metrics we care about
var (
    HTTPRequests = NewCounterVec("aos_http_requests_total", "Total HTTP requests", "method", "path", "status")
    HTTPDuration = NewSummaryVec("aos_http_request_seconds", "HTTP request duration seconds", "method", "path", "status")

    BusMessages  = NewCounterVec("aos_bus_messages_total", "Bus messages by target and result", "target", "result") // result=sent|dropped

    LLMPings     = NewCounterVec("aos_llm_pings_total", "LLM Ping calls", "provider", "outcome") // outcome=ok|error
    LLMChats     = NewCounterVec("aos_llm_chats_total", "LLM Chat calls", "provider", "outcome")
    LLMChatDur   = NewSummaryVec("aos_llm_chat_seconds", "LLM Chat duration seconds", "provider", "outcome")

    // Domain metrics (Chestertown observability, section 6). Labels take a small, fixed set of
    // values: never a task, user, pipeline or tool name.
    RunDuration  = NewHistogramVec("aos_run_duration_seconds", "One run, from its request (or resume) to its result or a wait for a person", RunBuckets, "outcome") // completed|blocked|error|timeout
    StepDuration = NewHistogramVec("aos_step_duration_seconds", "One tool call, retries included", StepBuckets, "tool_type", "outcome")
    GateDuration = NewHistogramVec("aos_gate_duration_seconds", "The call to Prudence's decision API", GateBuckets, "verdict") // ALLOW|DENY|APPROVAL_REQUIRED|ERROR
    EvidenceShipped = NewCounterVec("aos_evidence_shipped_total", "Audit records sent to Ernest", "transport", "outcome") // kafka|http, success|failure
    EvidenceBacklog = NewGaugeVec("aos_evidence_backlog_bytes", "Audit journal bytes not yet shipped to Ernest over Kafka", "transport")
)

// ServeHTTP exposes all metrics in Prometheus text format.
func ServeHTTP(w http.ResponseWriter, r *http.Request) {
    // OpenMetrics when the scraper asks for it (Prometheus does): the only format that carries
    // exemplars. Otherwise the classic text format.
    openMetrics := strings.Contains(r.Header.Get("Accept"), "application/openmetrics-text")
    if openMetrics {
        w.Header().Set("Content-Type", "application/openmetrics-text; version=1.0.0; charset=utf-8")
        defer fmt.Fprint(w, "# EOF\n")
    } else {
        w.Header().Set("Content-Type", "text/plain; version=0.0.4")
    }

    // helper to dump counter vec
    dumpCounter := func(cv *CounterVec) {
        // In OpenMetrics a counter's family name has no _total; its samples keep it.
        family := cv.Name
        if openMetrics {
            family = strings.TrimSuffix(cv.Name, "_total")
        }
        fmt.Fprintf(w, "# HELP %s %s\n", family, cv.Help)
        fmt.Fprintf(w, "# TYPE %s counter\n", family)
        cv.mu.RLock()
        for key, val := range cv.values {
            if key == "" {
                fmt.Fprintf(w, "%s %g\n", cv.Name, val)
            } else {
                fmt.Fprintf(w, "%s{%s} %g\n", cv.Name, key, val)
            }
        }
        cv.mu.RUnlock()
    }

    dumpSummary := func(sv *SummaryVec) {
        // Prometheus summary convention: name_sum and name_count
        fmt.Fprintf(w, "# HELP %s %s\n", sv.Name, sv.Help)
        fmt.Fprintf(w, "# TYPE %s summary\n", sv.Name)
        sv.mu.RLock()
        for key, cnt := range sv.count {
            sum := sv.sum[key]
            if key == "" {
                fmt.Fprintf(w, "%s_sum %g\n", sv.Name, sum)
                fmt.Fprintf(w, "%s_count %g\n", sv.Name, cnt)
            } else {
                fmt.Fprintf(w, "%s_sum{%s} %g\n", sv.Name, key, sum)
                fmt.Fprintf(w, "%s_count{%s} %g\n", sv.Name, key, cnt)
            }
        }
        sv.mu.RUnlock()
    }

    dumpHistogram := func(hv *HistogramVec) {
        fmt.Fprintf(w, "# HELP %s %s\n", hv.Name, hv.Help)
        fmt.Fprintf(w, "# TYPE %s histogram\n", hv.Name)
        hv.mu.RLock()
        for key, counts := range hv.counts {
            sep := ""
            if key != "" {
                sep = string(key) + ","
            }
            ex := hv.exemplars[key]
            suffix := func(i int) string {
                if !openMetrics || i >= len(ex) || ex[i].traceID == "" {
                    return ""
                }
                return fmt.Sprintf(" # {trace_id=\"%s\"} %g %.3f", ex[i].traceID, ex[i].value, float64(ex[i].at.UnixMilli())/1000)
            }
            for i, upper := range hv.Buckets {
                fmt.Fprintf(w, "%s_bucket{%sle=\"%g\"} %g%s\n", hv.Name, sep, upper, counts[i], suffix(i))
            }
            fmt.Fprintf(w, "%s_bucket{%sle=\"+Inf\"} %g%s\n", hv.Name, sep, counts[len(hv.Buckets)], suffix(len(hv.Buckets)))
            if key == "" {
                fmt.Fprintf(w, "%s_sum %g\n%s_count %g\n", hv.Name, hv.sum[key], hv.Name, counts[len(hv.Buckets)])
            } else {
                fmt.Fprintf(w, "%s_sum{%s} %g\n%s_count{%s} %g\n", hv.Name, key, hv.sum[key], hv.Name, key, counts[len(hv.Buckets)])
            }
        }
        hv.mu.RUnlock()
    }

    dumpGauge := func(gv *GaugeVec) {
        fmt.Fprintf(w, "# HELP %s %s\n", gv.Name, gv.Help)
        fmt.Fprintf(w, "# TYPE %s gauge\n", gv.Name)
        gv.mu.RLock()
        for key, val := range gv.values {
            if key == "" {
                fmt.Fprintf(w, "%s %g\n", gv.Name, val)
            } else {
                fmt.Fprintf(w, "%s{%s} %g\n", gv.Name, key, val)
            }
        }
        gv.mu.RUnlock()
    }

    dumpCounter(HTTPRequests)
    dumpSummary(HTTPDuration)
    dumpCounter(BusMessages)
    dumpCounter(LLMPings)
    dumpCounter(LLMChats)
    dumpSummary(LLMChatDur)
    dumpHistogram(RunDuration)
    dumpHistogram(StepDuration)
    dumpHistogram(GateDuration)
    dumpCounter(EvidenceShipped)
    dumpGauge(EvidenceBacklog)
    writeRuntime(w, openMetrics)
}
