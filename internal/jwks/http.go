package jwks

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
)

func NewHTTPClientWithCertBundles(patterns []string) (*http.Client, error) {
	if len(patterns) == 0 {
		return http.DefaultClient, nil
	}

	roots, err := x509.SystemCertPool()
	if err != nil {
		return nil, fmt.Errorf("load system certificate pool: %w", err)
	}
	if err := appendCertBundles(roots, patterns); err != nil {
		return nil, err
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	if transport.TLSClientConfig != nil {
		transport.TLSClientConfig = transport.TLSClientConfig.Clone()
	} else {
		transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	transport.TLSClientConfig.RootCAs = roots
	return &http.Client{Transport: transport}, nil
}

func appendCertBundles(pool *x509.CertPool, patterns []string) error {
	for _, pattern := range patterns {
		paths, err := filepath.Glob(pattern)
		if err != nil {
			return fmt.Errorf("expand certificate bundle pattern %q: %w", pattern, err)
		}
		if len(paths) == 0 {
			return fmt.Errorf("certificate bundle pattern %q matched no files", pattern)
		}
		for _, path := range paths {
			data, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("read certificate bundle %q: %w", path, err)
			}
			if err := appendPEMCertificates(pool, path, data); err != nil {
				return err
			}
		}
	}
	return nil
}

func appendPEMCertificates(pool *x509.CertPool, path string, data []byte) error {
	certificates := 0
	for len(data) > 0 {
		var block *pem.Block
		block, data = pem.Decode(data)
		if block == nil {
			for _, b := range data {
				if b != ' ' && b != '\t' && b != '\r' && b != '\n' {
					return fmt.Errorf("certificate bundle %q contains invalid PEM data", path)
				}
			}
			break
		}
		if block.Type != "CERTIFICATE" {
			return fmt.Errorf("certificate bundle %q contains PEM block %q, want CERTIFICATE", path, block.Type)
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return fmt.Errorf("parse certificate in bundle %q: %w", path, err)
		}
		pool.AddCert(cert)
		certificates++
	}
	if certificates == 0 {
		return fmt.Errorf("certificate bundle %q contains no certificates", path)
	}
	return nil
}
