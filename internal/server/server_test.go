package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type healthTestClient struct {
	harborClient
	pingErr error
}

func (c healthTestClient) Ping(context.Context) error {
	return c.pingErr
}

func TestHealthAccessLogging(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		pingErr    error
		wantStatus int
		wantBody   string
		wantLogs   []string
	}{
		{name: "healthy", method: http.MethodGet, path: "/api/v1/health?probe=1", wantStatus: http.StatusOK},
		{name: "unhealthy", method: http.MethodGet, path: "/api/v1/health", pingErr: errors.New("ping failed"), wantStatus: http.StatusServiceUnavailable, wantBody: "not healthy: failed to ping harbor\n", wantLogs: []string{"ping harbor"}},
		{name: "head", method: http.MethodHead, path: "/api/v1/health", wantStatus: http.StatusOK},
		{name: "similar path", method: http.MethodGet, path: "/api/v1/healthz", wantStatus: http.StatusNotFound, wantBody: "404 page not found\n", wantLogs: []string{"request"}},
		{name: "other path", method: http.MethodGet, path: "/missing", wantStatus: http.StatusNotFound, wantBody: "404 page not found\n", wantLogs: []string{"request"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, nil))
			s, err := NewServer(t.Context(), "127.0.0.1:0", logger, healthTestClient{pingErr: tt.pingErr}, nil, time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = s.listener.Close() })

			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(tt.method, tt.path, nil)
			s.server.Handler.ServeHTTP(recorder, req)
			if recorder.Code != tt.wantStatus || recorder.Body.String() != tt.wantBody {
				t.Errorf("response = %d %q, want %d %q", recorder.Code, recorder.Body.String(), tt.wantStatus, tt.wantBody)
			}

			decoder := json.NewDecoder(&logs)
			var messages []string
			for decoder.More() {
				var record struct {
					Message string `json:"msg"`
				}
				if err := decoder.Decode(&record); err != nil {
					t.Fatal(err)
				}
				messages = append(messages, record.Message)
			}
			if strings.Join(messages, ",") != strings.Join(tt.wantLogs, ",") {
				t.Errorf("log messages = %q, want %q", messages, tt.wantLogs)
			}
		})
	}
}

func TestStartWaitsForInFlightRequests(t *testing.T) {
	os.Clearenv() // Prevent proxy settings from redirecting local test traffic.
	s, err := NewServer(t.Context(), ":0", slog.New(slog.DiscardHandler), nil, nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	started := make(chan struct{})
	var finished atomic.Bool
	s.router.HandleFunc("GET /slow", func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		time.Sleep(200 * time.Millisecond)
		finished.Store(true)
	})

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- s.Start(ctx) }()

	go func() {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+s.listener.Addr().String()+"/slow", nil)
		if resp, err := http.DefaultClient.Do(req); err == nil {
			_ = resp.Body.Close()
		}
	}()

	<-started
	cancel()

	if err := <-done; err != nil {
		t.Fatalf("Start returned %v, want nil", err)
	}
	if !finished.Load() {
		t.Fatal("Start returned before the in-flight request finished")
	}
}

func TestSlowRequestHeadersAreCutOff(t *testing.T) {
	s, err := NewServer(t.Context(), "127.0.0.1:0", slog.New(slog.DiscardHandler), nil, nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if s.server.ReadHeaderTimeout != readHeaderTimeout {
		t.Errorf("ReadHeaderTimeout = %v, want %v", s.server.ReadHeaderTimeout, readHeaderTimeout)
	}
	s.server.ReadHeaderTimeout = 100 * time.Millisecond // don't make the test wait the full time

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- s.Start(ctx) }()

	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", s.listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()

	// The headers are never finished, as there's no blank line at the end
	if _, err := conn.Write([]byte("GET /api/v1/health HTTP/1.1\r\nHost: localhost\r\n")); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, err = io.Copy(io.Discard, conn) // returns once the server closes the connection
	if netErr, ok := errors.AsType[net.Error](err); ok && netErr.Timeout() {
		t.Fatal("server kept the connection open despite never getting the full request headers")
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Start returned %v, want nil", err)
	}
}
