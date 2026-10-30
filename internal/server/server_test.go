package server

import (
	"context"
	"log/slog"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func TestStartWaitsForInFlightRequests(t *testing.T) {
	s := NewServer("0", slog.New(slog.DiscardHandler))

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
