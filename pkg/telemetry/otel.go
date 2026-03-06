package telemetry

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/asciifaceman/lived/pkg/config"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

func Setup(ctx context.Context, cfg config.Config) (func(context.Context) error, error) {
	endpoint := strings.TrimSpace(cfg.OTELEndpoint)
	if endpoint == "" {
		return nil, fmt.Errorf("otlp endpoint is required when OTel is enabled")
	}

	exporterOptions := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(endpoint),
	}
	if cfg.OTELInsecure {
		exporterOptions = append(exporterOptions, otlptracegrpc.WithInsecure())
	}

	traceExporter, err := otlptracegrpc.New(ctx, exporterOptions...)
	if err != nil {
		return nil, err
	}

	serviceName := strings.TrimSpace(cfg.OTELServiceName)
	if serviceName == "" {
		serviceName = "lived"
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
			attribute.String("deployment.environment", strings.ToLower(strings.TrimSpace(getEnv("LIVED_ENV", "development")))),
		),
	)
	if err != nil {
		return nil, err
	}

	sampleRatio := cfg.OTELSampleRatio
	if sampleRatio < 0 {
		sampleRatio = 0
	}
	if sampleRatio > 1 {
		sampleRatio = 1
	}

	traceProvider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter,
			sdktrace.WithBatchTimeout(2*time.Second),
		),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(sampleRatio))),
	)

	otel.SetTracerProvider(traceProvider)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	return func(shutdownCtx context.Context) error {
		return traceProvider.Shutdown(shutdownCtx)
	}, nil
}

func getEnv(key string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}
