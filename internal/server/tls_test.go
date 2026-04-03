package server

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateSelfSignedCert(t *testing.T) {
	dir := t.TempDir()

	certFile, keyFile, err := GenerateSelfSignedCert(dir)
	if err != nil {
		t.Fatalf("GenerateSelfSignedCert: %v", err)
	}

	// Verify files exist.
	if _, err := os.Stat(certFile); err != nil {
		t.Fatalf("cert file does not exist: %v", err)
	}
	if _, err := os.Stat(keyFile); err != nil {
		t.Fatalf("key file does not exist: %v", err)
	}

	// Verify paths are in the expected directory.
	if filepath.Dir(certFile) != dir {
		t.Errorf("cert file dir = %q, want %q", filepath.Dir(certFile), dir)
	}
	if filepath.Dir(keyFile) != dir {
		t.Errorf("key file dir = %q, want %q", filepath.Dir(keyFile), dir)
	}

	// Parse and validate the certificate.
	certPEM, err := os.ReadFile(certFile)
	if err != nil {
		t.Fatalf("read cert file: %v", err)
	}

	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("failed to decode PEM block from cert file")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}

	// Check that localhost is a valid DNS name.
	foundLocalhost := false
	for _, name := range cert.DNSNames {
		if name == "localhost" {
			foundLocalhost = true
			break
		}
	}
	if !foundLocalhost {
		t.Errorf("cert DNS names = %v, want to contain 'localhost'", cert.DNSNames)
	}

	// Check that 127.0.0.1 is in IP addresses.
	found127 := false
	for _, ip := range cert.IPAddresses {
		if ip.String() == "127.0.0.1" {
			found127 = true
			break
		}
	}
	if !found127 {
		t.Errorf("cert IP addresses = %v, want to contain 127.0.0.1", cert.IPAddresses)
	}

	// Verify the key file is parseable.
	keyPEM, err := os.ReadFile(keyFile)
	if err != nil {
		t.Fatalf("read key file: %v", err)
	}
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		t.Fatal("failed to decode PEM block from key file")
	}
}
