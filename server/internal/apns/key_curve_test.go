package apns

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func syntheticKey(t *testing.T, curve elliptic.Curve) (*ecdsa.PrivateKey, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(curve, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "test-key.p8")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	return key, path
}

func TestProviderKeyRequiresP256(t *testing.T) {
	for _, tc := range []struct {
		name  string
		curve elliptic.Curve
	}{
		{"P224", elliptic.P224()}, {"P384", elliptic.P384()}, {"P521", elliptic.P521()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			key, path := syntheticKey(t, tc.curve)
			_, err := New(Config{KeyID: "EXAMPLE", TeamID: "EXAMPLE", KeyPath: path})
			if err == nil || !strings.Contains(err.Error(), "P-256") {
				t.Fatalf("New: %v, want P-256 error", err)
			}
			c := NewWithKey(Config{KeyID: "EXAMPLE", TeamID: "EXAMPLE"}, key)
			if _, err := c.token(); err == nil || !strings.Contains(err.Error(), "P-256") {
				t.Fatalf("token: %v, want P-256 error", err)
			}
		})
	}
}

func TestProviderP256TokenVerifies(t *testing.T) {
	key, path := syntheticKey(t, elliptic.P256())
	c, err := New(Config{KeyID: "EXAMPLE", TeamID: "EXAMPLE", KeyPath: path})
	if err != nil {
		t.Fatal(err)
	}
	token, err := c.token()
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token has %d parts", len(parts))
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(sig) != 64 {
		t.Fatalf("signature length=%d error=%v", len(sig), err)
	}
	hash := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if !ecdsa.Verify(&key.PublicKey, hash[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		t.Fatal("invalid ES256 signature")
	}
}

func TestInMemoryProviderKeyCannotPanicWhenMissing(t *testing.T) {
	for _, key := range []*ecdsa.PrivateKey{nil, {}} {
		c := NewWithKey(Config{KeyID: "EXAMPLE", TeamID: "EXAMPLE"}, key)
		if _, err := c.token(); err == nil || !strings.Contains(err.Error(), "P-256") {
			t.Fatalf("token: %v, want P-256 error", err)
		}
	}
}
