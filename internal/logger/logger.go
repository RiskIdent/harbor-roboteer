package logger

import (
	"context"
	"log/slog"
	"os"

	"go.opentelemetry.io/otel/trace"
)

func New(level, format string) *slog.Logger {
	h := withTraceContext(handler(format, stringToLevel(level)))
	return slog.New(h)
}

type traceContextHandler struct {
	next slog.Handler
}

func withTraceContext(next slog.Handler) slog.Handler {
	return traceContextHandler{next: next}
}

func (h traceContextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h traceContextHandler) Handle(ctx context.Context, record slog.Record) error {
	if spanContext := trace.SpanContextFromContext(ctx); spanContext.IsValid() {
		record.AddAttrs(
			slog.String("traceId", spanContext.TraceID().String()),
			slog.String("spanId", spanContext.SpanID().String()),
		)
	}
	return h.next.Handle(ctx, record)
}

func (h traceContextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return traceContextHandler{next: h.next.WithAttrs(attrs)}
}

func (h traceContextHandler) WithGroup(name string) slog.Handler {
	return traceContextHandler{next: h.next.WithGroup(name)}
}

func stringToLevel(level string) slog.Level {
	switch level {
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	case "debug":
		return slog.LevelDebug
	default:
		return slog.LevelInfo
	}
}

func handler(format string, level slog.Level) slog.Handler {
	switch format {
	case "text":
		return slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			AddSource: level == slog.LevelDebug,
			Level:     level,
		})
	default:
		return slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			AddSource: level == slog.LevelDebug,
			Level:     level,
		})
	}
}
