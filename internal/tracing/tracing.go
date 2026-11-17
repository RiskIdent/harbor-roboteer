package tracing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/RiskIdent/harbor-roboteer/internal/version"
	"github.com/go-logr/logr"
	"go.opentelemetry.io/contrib/exporters/autoexport"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.20.0"
)

// Initializes an OTLP exporter, and configures the corresponding trace provider.
func NewTracerProvider(ctx context.Context, logger *slog.Logger) (*tracesdk.TracerProvider, error) {
	logger = logger.WithGroup("otel")
	otel.SetLogger(logr.FromSlogHandler(logger.Handler()).V(8))

	if strings.EqualFold(os.Getenv("OTEL_SDK_DISABLED"), "true") {
		logger.DebugContext(ctx, "Tracing is disabled using OTEL_SDK_DISABLED.")
		return nil, nil
	}

	if !anyOTelEnvVar() {
		logger.DebugContext(ctx, "Tracing is disabled by default.",
			"hint",
			"To enable it, set any OTEL_* environment variable following the OpenTelemetry specification:\n"+
				"https://opentelemetry.io/docs/specs/otel/configuration/sdk-environment-variables/\n")
		return nil, nil
	}

	attributes := []attribute.KeyValue{
		semconv.ServiceName("harbor-roboteer"),
		semconv.ServiceVersion(version.Get()),
	}

	res, err := resource.New(ctx,
		resource.WithTelemetrySDK(),
		resource.WithAttributes(attributes...),
		resource.WithFromEnv(),
	)
	if err != nil {
		return nil, err
	}

	// Set up a trace exporter
	spanExporter, err := autoexport.NewSpanExporter(ctx)
	if err != nil {
		return nil, fmt.Errorf("create span exporter: %w", err)
	}

	if autoexport.IsNoneSpanExporter(spanExporter) {
		logger.DebugContext(ctx, "No tracing/span exporters configured")
	}

	// Register the trace exporter with a TracerProvider, using a batch
	// span processor to aggregate spans before export.
	bsp := tracesdk.NewBatchSpanProcessor(spanExporter)
	tracerProvider := tracesdk.NewTracerProvider(
		tracesdk.WithResource(res),
		tracesdk.WithSpanProcessor(bsp),
	)
	otel.SetTracerProvider(tracerProvider)

	// Set global propagator to tracecontext (the default is no-op).
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

	// Shutdown will flush any remaining spans and shut down the exporter.
	return tracerProvider, nil
}

func anyOTelEnvVar() bool {
	for _, env := range os.Environ() {
		if strings.HasPrefix(env, "OTEL_") {
			return true
		}
	}
	return false
}

func ShutdownTracer(ctx context.Context, logger *slog.Logger, traceProvider *tracesdk.TracerProvider) {
	if traceProvider == nil {
		return
	}
	logger = logger.WithGroup("otel")
	logger.DebugContext(ctx, "Shutting down tracer")
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err := traceProvider.Shutdown(ctx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		logger.WarnContext(ctx, "Error while shutting down trace exporter", "error", err)
	}
}
