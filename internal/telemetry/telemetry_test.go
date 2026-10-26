package telemetry

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

const stored = "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"

func TestATraceparentStoredWithARecordBecomesALink(t *testing.T) {
	links := linksOf(LinkTo(stored))
	if len(links) != 1 || links[0].SpanContext.TraceID().String() != "0af7651916cd43dd8448eb211c80319c" {
		t.Fatalf("links = %+v", links)
	}
	for _, bad := range []string{"", "garbage", "00-00000000000000000000000000000000-b7ad6b7169203331-01"} {
		if links := linksOf(LinkTo(bad)); len(links) != 0 {
			t.Errorf("LinkTo(%q) linked %+v", bad, links)
		}
	}
}

func TestTheTraceparentOfASpanIsStoredAndDetachedFromItsRequest(t *testing.T) {
	traceID, _ := trace.TraceIDFromHex("0af7651916cd43dd8448eb211c80319c")
	spanID, _ := trace.SpanIDFromHex("b7ad6b7169203331")
	ctx, cancel := context.WithCancel(trace.ContextWithSpanContext(context.Background(),
		trace.NewSpanContext(trace.SpanContextConfig{TraceID: traceID, SpanID: spanID, TraceFlags: trace.FlagsSampled})))
	detached := Detach(ctx)
	cancel()
	if detached.Err() != nil {
		t.Fatal("the task's context ended with its request")
	}
	if got := Traceparent(detached); got != stored {
		t.Fatalf("Traceparent = %q", got)
	}
	if TraceID(detached) != traceID.String() || Traceparent(context.Background()) != "" || TraceID(context.Background()) != "" {
		t.Fatal("trace ids")
	}
}

func TestTracingIsOffWithoutAnEndpoint(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
	if Enabled() {
		t.Fatal("enabled without an endpoint")
	}
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://collector:4318")
	if !Enabled() {
		t.Fatal("disabled with an endpoint")
	}
	t.Setenv("OTEL_SDK_DISABLED", "true")
	if Enabled() {
		t.Fatal("enabled with OTEL_SDK_DISABLED")
	}
}

func linksOf(opt trace.SpanStartOption) []trace.Link {
	cfg := trace.NewSpanStartConfig(opt)
	return cfg.Links()
}
