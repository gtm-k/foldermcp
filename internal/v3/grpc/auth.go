//go:build cgo

package grpc

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

// AuthPaths holds paths to auth material on disk.
type AuthPaths struct {
	Dir       string // ~/.foldermcp/auth/
	TokenFile string // token.hex
	CertFile  string // cert.pem
	KeyFile   string // key.pem
}

// DefaultAuthPaths returns the conventional auth directory layout.
func DefaultAuthPaths(homeDir string) AuthPaths {
	base := filepath.Join(homeDir, ".foldermcp", "auth")
	return AuthPaths{
		Dir:       base,
		TokenFile: filepath.Join(base, "token.hex"),
		CertFile:  filepath.Join(base, "cert.pem"),
		KeyFile:   filepath.Join(base, "key.pem"),
	}
}

// Init creates a 256-bit random token and a 1-year self-signed ECDSA P-256
// TLS certificate. All files are mode 0600; the directory is mode 0700.
// Idempotent: returns nil if token already exists.
func Init(paths AuthPaths) error {
	if err := os.MkdirAll(paths.Dir, 0700); err != nil {
		return fmt.Errorf("mkdir auth: %w", err)
	}

	if _, err := os.Stat(paths.TokenFile); err == nil {
		return nil // already initialized
	}

	// Generate 256-bit token
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return fmt.Errorf("rand token: %w", err)
	}
	if err := os.WriteFile(paths.TokenFile, []byte(hex.EncodeToString(tokenBytes)), 0600); err != nil {
		return fmt.Errorf("write token: %w", err)
	}

	// Generate ECDSA P-256 key
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("gen key: %w", err)
	}
	keyBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return fmt.Errorf("marshal key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})
	if err := os.WriteFile(paths.KeyFile, keyPEM, 0600); err != nil {
		return fmt.Errorf("write key: %w", err)
	}

	// Generate self-signed cert valid for 1 year
	template := x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "foldermcp-indexd"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().AddDate(1, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return fmt.Errorf("gen cert: %w", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	if err := os.WriteFile(paths.CertFile, certPEM, 0600); err != nil {
		return fmt.Errorf("write cert: %w", err)
	}
	return nil
}

// Rotate replaces the token with a new random value. Cert is left in place.
func Rotate(paths AuthPaths) error {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return fmt.Errorf("rand token: %w", err)
	}
	return os.WriteFile(paths.TokenFile, []byte(hex.EncodeToString(tokenBytes)), 0600)
}
