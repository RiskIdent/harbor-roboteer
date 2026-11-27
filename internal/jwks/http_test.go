package jwks

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAppendCertBundlesGlob(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	expected := x509.NewCertPool()
	expected.AddCert(writeTestCertificate(t, filepath.Join(dir, "one.pem"), 1))
	expected.AddCert(writeTestCertificate(t, filepath.Join(dir, "two.pem"), 2))
	pool := x509.NewCertPool()

	if err := appendCertBundles(pool, []string{filepath.Join(dir, "*.pem")}); err != nil {
		t.Fatal(err)
	}
	if !pool.Equal(expected) {
		t.Error("loaded certificate pool does not match the certificate bundles")
	}
}

func TestAppendCertBundlesErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	t.Run("no matches", func(t *testing.T) {
		err := appendCertBundles(x509.NewCertPool(), []string{filepath.Join(dir, "*.missing")})
		if err == nil {
			t.Fatal("want error")
		}
	})
	t.Run("invalid glob", func(t *testing.T) {
		err := appendCertBundles(x509.NewCertPool(), []string{"["})
		if err == nil {
			t.Fatal("want error")
		}
	})
	t.Run("DER", func(t *testing.T) {
		path := filepath.Join(dir, "certificate.der")
		writeFile(t, path, []byte("not PEM"))
		err := appendCertBundles(x509.NewCertPool(), []string{path})
		if err == nil {
			t.Fatal("want error")
		}
	})
	t.Run("non-certificate PEM", func(t *testing.T) {
		path := filepath.Join(dir, "private.pem")
		writeFile(t, path, pem.EncodeToMemory(&pem.Block{
			Type:  "PRIVATE KEY",
			Bytes: []byte("not a certificate"),
		}))
		err := appendCertBundles(x509.NewCertPool(), []string{path})
		if err == nil {
			t.Fatal("want error")
		}
	})
}

func writeTestCertificate(t *testing.T, path string, serial int64) *x509.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(serial),
		Subject:               pkix.Name{CommonName: filepath.Base(path)},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
