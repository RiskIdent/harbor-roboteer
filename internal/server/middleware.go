package server

import (
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/trace"
)

type Middleware func(http.Handler) http.Handler

// ChainMiddlewares wraps list of middlewares. The first middleware in the list is the outermost.
func ChainMiddlewares(h http.Handler, mws ...Middleware) http.Handler {
	for _, mw := range slices.Backward(mws) {
		h = mw(h)
	}
	return h
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// Unwrap lets http.NewResponseController reach the underlying writer.
func (rw *responseWriter) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}

func LoggingMiddleware(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return loggingHandler(logger, next)
	}
}

func loggingHandler(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Create a custom response writer to capture status code
		rw := &responseWriter{
			ResponseWriter: w,
			statusCode:     http.StatusOK,
		}

		next.ServeHTTP(rw, r)

		duration := time.Since(start)

		logLevel := slog.LevelInfo
		if rw.statusCode >= 500 {
			logLevel = slog.LevelError
		} else if rw.statusCode >= 400 && rw.statusCode < 500 {
			logLevel = slog.LevelWarn
		}

		attrs := []slog.Attr{
			slog.Int("status", rw.statusCode),
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.String("duration", duration.String()),
			slog.String("remoteAddr", r.RemoteAddr),
			slog.String("userAgent", r.UserAgent()),
		}
		// Only set when the tracing middleware started a span (filtered paths have none)
		if sc := trace.SpanContextFromContext(r.Context()); sc.IsValid() {
			attrs = append(attrs,
				slog.String("traceId", sc.TraceID().String()),
				slog.String("spanId", sc.SpanID().String()),
			)
		}

		logger.LogAttrs(r.Context(), logLevel, "request", attrs...)
	})
}

func TracingMiddleware(untracedPaths ...string) Middleware {
	return otelhttp.NewMiddleware("server", otelhttp.WithFilter(func(r *http.Request) bool {
		return !slices.ContainsFunc(untracedPaths, func(pattern string) bool {
			return matchPath(pattern, r.URL.Path)
		})
	}))
}

func matchPath(pattern, path string) bool {
	if prefix, ok := strings.CutSuffix(pattern, "*"); ok {
		return strings.HasPrefix(path, prefix)
	}
	return path == pattern
}
