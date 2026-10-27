// Package telemetry is AOS's OpenTelemetry tracing: a tracer provider exporting OTLP over HTTP
// when an endpoint is configured, W3C trace context for propagation, and helpers to carry a
// trace context through the places a span cannot stay open (the audit journal, Kafka, a background retry).
//
// Everything is configured by the standard OTEL_* variables. Without
// OTEL_EXPORTER_OTLP_ENDPOINT (or OTEL_EXPORTER_OTLP_TRACES_ENDPOINT) no span is recorded or
// exported, but an incoming traceparent is still propagated to the calls AOS makes.
package telemetry

import (
	"context"
	"net/http"
	"os"
	"strings"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// CorrelationKey is the attribute and log field naming a task's business flow: its task id.
const CorrelationKey = "correlation_id"

const instrumentation = "github.com/ccastromar/aos-agentic-orchestration-system"

// Only W3C trace context: baggage is forwarded to third parties, and the correlation id travels
// in its own header (X-Correlation-Id).
var propagator = propagation.TraceContext{}

func init() {
	otel.SetTextMapPropagator(propagator)
}

// Enabled says whether spans are exported: an OTLP endpoint is configured and the SDK is not
// disabled.
func Enabled() bool {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("OTEL_SDK_DISABLED")), "true") {
		return false
	}
	return strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")) != "" ||
		strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT")) != ""
}

// Setup installs the tracer provider when Enabled, and returns the function that flushes and
// stops it. service.name defaults to "aos" and service.namespace to "chestertown"; both, and
// the rest of the resource, can be overridden with OTEL_SERVICE_NAME and
// OTEL_RESOURCE_ATTRIBUTES.
func Setup(ctx context.Context, version string) (func(context.Context) error, error) {
	if !Enabled() {
		return func(context.Context) error { return nil }, nil
	}
	exporter, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, err
	}
	if version == "" {
		version = "dev"
	}
	defaults := resource.NewSchemaless(
		attribute.String("service.name", "aos"),
		attribute.String("service.namespace", "chestertown"),
		attribute.String("service.version", version),
	)
	// Merged after the defaults, so the environment wins.
	detected, err := resource.New(ctx, resource.WithFromEnv(), resource.WithTelemetrySDK())
	if err != nil {
		return nil, err
	}
	res, err := resource.Merge(defaults, detected)
	if err != nil {
		return nil, err
	}
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(provider)
	return provider.Shutdown, nil
}

// Tracer is AOS's tracer (a no-op until Setup installs a provider).
func Tracer() trace.Tracer {
	return otel.Tracer(instrumentation)
}

// HTTPClient makes outgoing calls with a client span and the traceparent of the caller's span.
var HTTPClient = &http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)}

// Detach is a context that carries ctx's span context but none of its deadline or
// cancellation: for work a request starts and that outlives it.
func Detach(ctx context.Context) context.Context {
	return trace.ContextWithSpanContext(context.Background(), trace.SpanContextFromContext(ctx))
}

// Traceparent is the W3C traceparent of ctx's span, or "" when there is none, to store with a
// record (a journal entry, a Kafka record) whose delivery happens later.
func Traceparent(ctx context.Context) string {
	carrier := propagation.MapCarrier{}
	propagator.Inject(ctx, carrier)
	return carrier.Get("traceparent")
}

// TraceID is ctx's trace id for a log line, or "".
func TraceID(ctx context.Context) string {
	if sc := trace.SpanContextFromContext(ctx); sc.HasTraceID() {
		return sc.TraceID().String()
	}
	return ""
}

// SampledTraceID is ctx's trace id when its span is sampled (recorded and exported), else "":
// the exemplar a measurement carries, so a histogram bucket leads to a trace that exists.
func SampledTraceID(ctx context.Context) string {
	if sc := trace.SpanContextFromContext(ctx); sc.IsSampled() {
		return sc.TraceID().String()
	}
	return ""
}

// LinkTo is the span option that links a new span to a stored traceparent: the delivery of a
// record is a new span, linked to the one that wrote it, never a span kept open while it waits.
func LinkTo(traceparent string) trace.SpanStartOption {
	if traceparent == "" {
		return trace.WithLinks()
	}
	sc := trace.SpanContextFromContext(propagator.Extract(context.Background(), propagation.MapCarrier{"traceparent": traceparent}))
	if !sc.IsValid() {
		return trace.WithLinks()
	}
	return trace.WithLinks(trace.Link{SpanContext: sc})
}
