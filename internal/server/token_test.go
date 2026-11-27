package server

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"net/http/httptest"
	"testing"

	"github.com/lestrrat-go/jwx/v4/jwa"
	"github.com/lestrrat-go/jwx/v4/jwk"
	"github.com/lestrrat-go/jwx/v4/jwt"
)

func TestParseJWTSelectsIssuerKeySet(t *testing.T) {
	const audience = "harbor-roboteer"
	issuerOne := "https://issuer-one.example.com"
	issuerTwo := "https://issuer-two.example.com"
	keyOne, optionsOne := newIssuerJWTOptions(t, issuerOne, audience)
	keyTwo, optionsTwo := newIssuerJWTOptions(t, issuerTwo, audience)
	s := &Server{ServerOptions: ServerOptions{
		JWTOptionsByIssuer: map[string][]jwt.ParseOption{
			issuerOne: optionsOne,
			issuerTwo: optionsTwo,
		},
	}}

	tests := []struct {
		name      string
		key       jwk.Key
		issuer    string
		wantError bool
	}{
		{name: "first issuer", key: keyOne, issuer: issuerOne},
		{name: "second issuer", key: keyTwo, issuer: issuerTwo},
		{name: "claim cannot select another issuer key set", key: keyOne, issuer: issuerTwo, wantError: true},
		{name: "unknown issuer", key: keyOne, issuer: "https://unknown.example.com", wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			token, err := jwt.NewBuilder().
				Issuer(test.issuer).
				Audience([]string{audience}).
				Build()
			if err != nil {
				t.Fatal(err)
			}
			signed, err := jwt.Sign(token, jwt.WithKey(jwa.ES256(), test.key))
			if err != nil {
				t.Fatal(err)
			}

			req := httptest.NewRequestWithContext(t.Context(), "POST", "/api/v1/robot", nil)
			req.Header.Set("Authorization", "Bearer "+string(signed))
			_, err = s.parseJWT(req)
			if (err != nil) != test.wantError {
				t.Fatalf("parseJWT() error = %v, wantError %v", err, test.wantError)
			}
		})
	}
}

func newIssuerJWTOptions(t *testing.T, issuer, audience string) (jwk.Key, []jwt.ParseOption) {
	t.Helper()
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := jwk.Import[jwk.Key](private)
	if err != nil {
		t.Fatal(err)
	}
	const keyID = "shared-key-id"
	if err := key.Set(jwk.KeyIDKey, keyID); err != nil {
		t.Fatal(err)
	}
	public, err := key.PublicKey()
	if err != nil {
		t.Fatal(err)
	}
	if err := public.Set(jwk.AlgorithmKey, jwa.ES256()); err != nil {
		t.Fatal(err)
	}
	set := jwk.NewSet()
	if err := set.AddKey(public); err != nil {
		t.Fatal(err)
	}
	return key, []jwt.ParseOption{
		jwt.WithKeySet(set),
		jwt.WithIssuer(issuer),
		jwt.WithAudience(audience),
	}
}
