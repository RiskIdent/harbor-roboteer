package harbor

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/goharbor/go-client/pkg/sdk/v2.0/client/ping"
)

func TestNew(t *testing.T) {
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

	c, err := New(srv.URL+"/", "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.api.Ping.GetPing(t.Context(), ping.NewGetPingParams()); err != nil {
		t.Fatal(err)
	}
}

func TestNewInvalidURL(t *testing.T) {
	for _, u := range []string{"", "harbor.example.com", "://bad"} {
		if _, err := New(u, "admin", "secret"); err == nil {
			t.Errorf("New(%q) returned no error", u)
		}
	}
}
