package server

import (
	"context"

	oteltrace "go.opentelemetry.io/otel/trace"
)

func traceLogAttrs(ctx context.Context) []any {
	spanContext := oteltrace.SpanContextFromContext(ctx)
	if !spanContext.IsValid() {
		return nil
	}

	return []any{
		"trace_id", spanContext.TraceID().String(),
		"span_id", spanContext.SpanID().String(),
		"trace_sampled", spanContext.IsSampled(),
	}
}
