## OpenTelemetry Package

### TODO: Add details about sending data and viewing it with specific backends (e.g. SigNoz or ClickStack)

This package provides common functionality for initializing OpenTelemetry SDKs within other applications.
Currently it only supports traces but may in the future also support metrics and logs.

## Usage
In your `main.go` file you can create a global tracer provider as follows:
```go
appOTelConfig := otel.OTelConfig {
    Enabled: true,
    Endpoint: example.com,
    ServiceName: "example-service",
}
otel.InitTracer(appOTelConfig)
```

Then in your service implementation code you can create a tracer and use it to generate spans in any method.
Be sure to propogate `ctx` properly so that spans can be linked into traces.
```go
var tracer = otel.Tracer("service")

func exampleHandler(ctx context.Context, input str) {
    _, span := tracer.Start(ctx, "exampleHandler",
		trace.WithAttributes(attribute.String("input", input)))
	defer span.End()
    /// ...
}
```