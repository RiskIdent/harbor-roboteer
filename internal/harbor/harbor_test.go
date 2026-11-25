package harbor

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/goharbor/go-client/pkg/sdk/v2.0/client/ping"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestNew(t *testing.T) {
	os.Clearenv() // Prevent proxy settings from redirecting local test traffic.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2.0/ping" {
			t.Errorf("path = %q, want /api/v2.0/ping", r.URL.Path)
		}
		if u, p, ok := r.BasicAuth(); !ok || u != "admin" || p != "secret" {
			t.Errorf("basic auth = %q/%q/%v, want admin/secret", u, p, ok)
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("Pong"))
	}))
	defer srv.Close()

	c, err := New(mustParseURL(srv.URL+"/"), "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.api.Ping.GetPing(t.Context(), ping.NewGetPingParams()); err != nil {
		t.Fatal(err)
	}
}

func TestHarborTracing(t *testing.T) {
	os.Clearenv() // Prevent proxy settings from redirecting local test traffic.
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previous)
		_ = provider.Shutdown(t.Context())
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2.0/ping":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("Pong"))
		case "/api/v2.0/robots/1":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":1}`))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c, err := New(mustParseURL(srv.URL), "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	ctx, parent := provider.Tracer("test").Start(t.Context(), "parent")
	defer parent.End()
	if err := c.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	if spans := recorder.Ended(); len(spans) != 0 {
		t.Fatalf("ping created %d spans, want none", len(spans))
	}
	if _, err := c.GetRobotByID(ctx, 1); err != nil {
		t.Fatal(err)
	}
	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("robot created %d spans, want one", len(spans))
	}
	if spans[0].Name() != "GetRobotByID" {
		t.Errorf("robot span name = %q, want GetRobotByID", spans[0].Name())
	}
	if spans[0].Parent().SpanID() != parent.SpanContext().SpanID() {
		t.Error("robot span is not a child of the caller's span")
	}
}

func TestNewInvalidURL(t *testing.T) {
	for _, u := range []string{"", "harbor.example.com"} {
		if _, err := New(mustParseURL(u), "admin", "secret"); err == nil {
			t.Errorf("New(%q) returned no error", u)
		}
	}
}

func mustParseURL(val string) *url.URL {
	u, err := url.Parse(val)
	if err != nil {
		panic(err)
	}
	return u
}

func TestDaysUntil(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	const day = 24 * time.Hour
	tests := []struct {
		remaining time.Duration
		want      int64
	}{
		{-2 * day, 1},
		{0, 1},
		{time.Nanosecond, 1},
		{day - time.Nanosecond, 1},
		{day, 1},
		{day + time.Nanosecond, 2},
		{2 * day, 2},
		{2*day + time.Hour, 3},
	}
	for _, test := range tests {
		if got := daysUntil(now, now.Add(test.remaining)); got != test.want {
			t.Errorf("daysUntil with %v remaining = %d, want %d", test.remaining, got, test.want)
		}
	}
}
