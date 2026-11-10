package jwks

import (
	"cmp"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v4/jwk"
)

// fakeProvider serves an OpenID configuration pointing at /keys, which keys serves.
// An empty reportedIssuer makes the configuration report the server's own URL.
func fakeProvider(t *testing.T, reportedIssuer string, keys http.Handler) string {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]string{
				"issuer":   cmp.Or(reportedIssuer, srv.URL),
				"jwks_uri": srv.URL + "/keys",
			})
		case "/keys":
			keys.ServeHTTP(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestNew(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := jwk.Import[jwk.Key](priv.Public())
	if err != nil {
		t.Fatal(err)
	}
	if err := key.Set(jwk.KeyIDKey, "test-kid"); err != nil {
		t.Fatal(err)
	}
	keys := jwk.NewSet()
	if err := keys.AddKey(key); err != nil {
		t.Fatal(err)
	}

	issuer := fakeProvider(t, "", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(keys)
	}))

	set, err := New(t.Context(), issuer)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := set.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})

	if _, ok := set.LookupKeyID("test-kid"); !ok {
		t.Fatalf("key test-kid not found, set has %d keys", set.Len())
	}
}

func TestNewFailsOnIssuerMismatch(t *testing.T) {
	issuer := fakeProvider(t, "https://evil.example.com", http.NotFoundHandler())
	if _, err := New(t.Context(), issuer); err == nil {
		t.Fatal("want error when the provider reports a different issuer")
	}
}

func TestNewFailsWhenNoDiscovery(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	if _, err := New(t.Context(), srv.URL); err == nil {
		t.Fatal("want error when the discovery endpoint returns 404")
	}
}

func TestNewFailsWhenKeysUnreachable(t *testing.T) {
	firstFetchTimeout = 500 * time.Millisecond
	t.Cleanup(func() { firstFetchTimeout = 30 * time.Second })

	issuer := fakeProvider(t, "", http.NotFoundHandler())
	if _, err := New(t.Context(), issuer); err == nil {
		t.Fatal("want error when the JWKS endpoint returns 404")
	}
}
