package server

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func TestStartWaitsForInFlightRequests(t *testing.T) {
	s, err := NewServer(t.Context(), ":0", slog.New(slog.DiscardHandler), nil)
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
	s, err := NewServer(t.Context(), "127.0.0.1:0", slog.New(slog.DiscardHandler), nil)
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
	if _, err := conn.Write([]byte("GET /health HTTP/1.1\r\nHost: localhost\r\n")); err != nil {
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
