package server

import (
	"cmp"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
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
	statusCode   int
	wroteHeader  bool
	bytesWritten int64
}

func (rw *responseWriter) WriteHeader(code int) {
	if !rw.wroteHeader {
		rw.statusCode, rw.wroteHeader = code, true
	}
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.wroteHeader {
		rw.WriteHeader(http.StatusOK)
	}
	n, err := rw.ResponseWriter.Write(b)
	rw.bytesWritten += int64(n)
	return n, err
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
	skip := matchesAny(untracedPaths)
	return otelhttp.NewMiddleware("server", otelhttp.WithFilter(func(r *http.Request) bool {
		return !skip(r)
	}))
}

func MetricsMiddleware(reg prometheus.Registerer, skipPaths ...string) Middleware {
	labels := []string{"route", "code"}
	requests := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "HTTP requests by route and status code.",
	}, labels)
	duration := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "HTTP request latency by route and status code.",
		Buckets: prometheus.DefBuckets,
	}, labels)
	sizeBuckets := prometheus.ExponentialBuckets(100, 10, 6) // 100 B .. 10 MB
	requestSize := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_size_bytes",
		Help:    "HTTP request body size by route and status code.",
		Buckets: sizeBuckets,
	}, labels)
	responseSize := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_response_size_bytes",
		Help:    "HTTP response body size by route and status code.",
		Buckets: sizeBuckets,
	}, labels)
	inflight := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "http_requests_in_flight",
		Help: "HTTP requests currently being served.",
	})
	reg.MustRegister(requests, duration, requestSize, responseSize, inflight)
	skip := matchesAny(skipPaths)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if skip(r) {
				next.ServeHTTP(w, r)
				return
			}

			inflight.Inc()
			rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
			start := time.Now()

			defer func() {
				inflight.Dec()
				route := cmp.Or(r.Pattern, "unmatched")
				code := strconv.Itoa(rw.statusCode)
				requests.WithLabelValues(route, code).Inc()
				duration.WithLabelValues(route, code).Observe(time.Since(start).Seconds())
				responseSize.WithLabelValues(route, code).Observe(float64(rw.bytesWritten))
				if r.ContentLength >= 0 {
					requestSize.WithLabelValues(route, code).Observe(float64(r.ContentLength))
				}
			}()

			next.ServeHTTP(rw, r)
		})
	}
}

func matchesAny(patterns []string) func(*http.Request) bool {
	return func(r *http.Request) bool {
		return slices.ContainsFunc(patterns, func(pattern string) bool {
			return matchPath(pattern, r.URL.Path)
		})
	}
}

func matchPath(pattern, path string) bool {
	if prefix, ok := strings.CutSuffix(pattern, "*"); ok {
		return strings.HasPrefix(path, prefix)
	}
	return path == pattern
}
