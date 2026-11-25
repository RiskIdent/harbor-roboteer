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
		rw.statusCode = code
		rw.wroteHeader = true
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

func LoggingMiddleware(logger *slog.Logger, unloggedPaths ...string) Middleware {
	skip := matchesAny(unloggedPaths)
	return func(next http.Handler) http.Handler {
		return loggingHandler(logger, next, skip)
	}
}

func loggingHandler(logger *slog.Logger, next http.Handler, skip func(*http.Request) bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if skip(r) {
			next.ServeHTTP(w, r)
			return
		}

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
		logger.LogAttrs(r.Context(), logLevel, "request", attrs...)
	})
}

func TracingMiddleware(untracedPaths ...string) Middleware {
	skip := matchesAny(untracedPaths)
	return otelhttp.NewMiddleware("server", otelhttp.WithFilter(func(r *http.Request) bool {
		return !skip(r)
	}))
}

type httpMetrics struct {
	requests     *prometheus.CounterVec
	duration     *prometheus.HistogramVec
	requestSize  *prometheus.HistogramVec
	responseSize *prometheus.HistogramVec
	inflight     prometheus.Gauge
	skip         func(*http.Request) bool
}

func newHTTPMetrics(skipPaths ...string) *httpMetrics {
	labels := []string{"route", "code"}
	sizeBuckets := prometheus.ExponentialBuckets(100, 10, 6) // 100 B .. 10 MB
	return &httpMetrics{
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "HTTP requests by route and status code.",
		}, labels),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request latency by route and status code.",
			Buckets: prometheus.DefBuckets,
		}, labels),
		requestSize: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "http_request_size_bytes",
			Help:    "HTTP request body size by route and status code.",
			Buckets: sizeBuckets,
		}, labels),
		responseSize: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "http_response_size_bytes",
			Help:    "HTTP response body size by route and status code.",
			Buckets: sizeBuckets,
		}, labels),
		inflight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "http_requests_in_flight",
			Help: "HTTP requests currently being served.",
		}),
		skip: matchesAny(skipPaths),
	}
}

func (m *httpMetrics) collectors() []prometheus.Collector {
	return []prometheus.Collector{m.requests, m.duration, m.requestSize, m.responseSize, m.inflight}
}

func (m *httpMetrics) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if m.skip(r) {
			next.ServeHTTP(w, r)
			return
		}

		m.inflight.Inc()
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		start := time.Now()

		defer func() {
			m.inflight.Dec()
			route := cmp.Or(r.Pattern, "unmatched")
			code := strconv.Itoa(rw.statusCode)
			m.requests.WithLabelValues(route, code).Inc()
			m.duration.WithLabelValues(route, code).Observe(time.Since(start).Seconds())
			m.responseSize.WithLabelValues(route, code).Observe(float64(rw.bytesWritten))
			// shortcut: chunked uploads have no Content-Length (-1) and are not observed, count body reads if they matter
			if r.ContentLength >= 0 {
				m.requestSize.WithLabelValues(route, code).Observe(float64(r.ContentLength))
			}
		}()

		next.ServeHTTP(rw, r)
	})
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
