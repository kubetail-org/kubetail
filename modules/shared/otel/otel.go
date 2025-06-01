package otel

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// OTelConfig is the configuration for the OTel tracer provider
type OTelConfig struct {
	Enabled bool
	// Debug mode, set to true to use stdout trace exporter
	Debug bool
	// TODO: Add support for a custom collector endpoint
	Endpoint    string
	ServiceName string
}

// InitTracer initializes the global OTel tracer provider
// which can be used to create tracers throughout the call stack
func InitTracer(cfg *OTelConfig) error {
	// No tracing enabled, set noop tracer provider
	if !cfg.Enabled {
		// Set the global TracerProvider to a NoopTracerProvider
		// this ensures that methods that start a span will not break despite
		// creating spans and no telemetry will be collected or sent
		otel.SetTracerProvider(noop.NewTracerProvider())
		return nil
	}

	// Define common attributes for all spans
	resources, err := resource.New(
		context.Background(),
		resource.WithAttributes(
			attribute.String("service.name", cfg.ServiceName),
			attribute.String("library.language", "go"),
		),
	)
	if err != nil {
		return err
	}

	// Debug mode, use stdout trace exporter
	if cfg.Debug {
		traceExporter, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
		if err != nil {
			return err
		}
		traceProvider := trace.NewTracerProvider(
			trace.WithSampler(trace.AlwaysSample()),
			trace.WithBatcher(traceExporter,
				trace.WithBatchTimeout(time.Second)),
			trace.WithResource(resources))
		otel.SetTracerProvider(traceProvider)
		return nil
	}

	// Development mode use gRPC trace exporter to an OTel collector without TLS
	// since this is a local development setup only for now.
	var traceExporter trace.SpanExporter
	if cfg.Endpoint != "" {
		// Use custom endpoint
		traceExporter, err = otlptracegrpc.New(context.Background(),
			otlptracegrpc.WithInsecure(),
			otlptracegrpc.WithEndpoint(cfg.Endpoint))
	} else {
		// Use default endpoint (localhost:4317)
		traceExporter, err = otlptracegrpc.New(context.Background(), otlptracegrpc.WithInsecure())
	}
	if err != nil {
		return err
	}
	traceProvider := trace.NewTracerProvider(
		trace.WithSampler(trace.AlwaysSample()),
		trace.WithBatcher(traceExporter,
			trace.WithBatchTimeout(time.Second)),
		trace.WithResource(resources))
	otel.SetTracerProvider(traceProvider)
	return nil
}
