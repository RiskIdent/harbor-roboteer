package jwks

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/jwx-go/jwkfetch/v4"
	"github.com/lestrrat-go/httprc/v3"
	"github.com/lestrrat-go/jwx/v4/jwk"
)

var firstFetchTimeout = 30 * time.Second

type Keys struct {
	jwk.Set
	cache *jwkfetch.Cache
}

func (k *Keys) Close(ctx context.Context) error {
	return k.cache.Shutdown(ctx)
}

// discover reads the issuer's OpenID configuration and returns its JWKS URL.
func discover(ctx context.Context, issuer string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, firstFetchTimeout)
	defer cancel()

	u, err := url.JoinPath(issuer, ".well-known/openid-configuration")
	if err != nil {
		return "", fmt.Errorf("discover: issuer %q: %w", issuer, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", fmt.Errorf("discover %s: %w", u, err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("discover %s: %w", u, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("discover %s: status %s", u, resp.Status)
	}

	var doc struct {
		Issuer  string `json:"issuer"`
		JWKSURI string `json:"jwks_uri"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&doc); err != nil {
		return "", fmt.Errorf("discover %s: decode: %w", u, err)
	}
	if doc.Issuer != issuer {
		return "", fmt.Errorf("discover %s: issuer mismatch: configured %q, provider says %q", u, issuer, doc.Issuer)
	}
	if doc.JWKSURI == "" {
		return "", fmt.Errorf("discover %s: no jwks_uri", u)
	}
	return doc.JWKSURI, nil
}

// New discovers the issuer's JWKS URL and keeps its key set fresh until stop is called.
// It fails if discovery or the first key fetch fails, so a bad issuer stops startup.
func New(ctx context.Context, issuer string) (*Keys, error) {
	jwksURL, err := discover(ctx, issuer)
	if err != nil {
		return nil, err
	}

	cache, err := jwkfetch.NewCache(ctx, httprc.NewClient())
	if err != nil {
		return nil, fmt.Errorf("start jwks cache: %w", err)
	}

	regCtx, cancel := context.WithTimeout(ctx, firstFetchTimeout)
	defer cancel()
	if err := cache.Register(regCtx, jwksURL,
		jwkfetch.WithMinInterval(5*time.Minute),
		jwkfetch.WithMaxInterval(25*time.Hour),
	); err != nil {
		_ = cache.Shutdown(context.WithoutCancel(ctx))
		return nil, fmt.Errorf("fetch jwks %s: %w", jwksURL, err)
	}

	set, err := cache.CachedSet(jwksURL)
	if err != nil {
		_ = cache.Shutdown(context.WithoutCancel(ctx))
		return nil, fmt.Errorf("get cached jwks %s: %w", jwksURL, err)
	}
	return &Keys{
		set,
		cache,
	}, nil
}
