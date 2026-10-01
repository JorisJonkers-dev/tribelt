// Package integrations connects the mirror to Google Search Console, Bing Webmaster Tools, IndexNow and
// Cloudflare analytics: credentials an admin enters in /stats (sealed in Postgres), the Vault-managed
// fallback, the scheduled imports and the audit log.
package integrations

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
)

// Sealing errors. Neither carries any detail of the credential.
var (
	ErrKeyChanged = errors.New("integrations: credential sealed under another key")
	ErrTampered   = errors.New("integrations: credential does not open")
	ErrShortKey   = errors.New("integrations: SESSION_KEY must be at least 32 characters")
)

const hkdfInfo = "tribelt-integrations-v1"

// Sealer encrypts credentials with AES-256-GCM under a key derived from SESSION_KEY (ADR-0005).
type Sealer struct {
	aead  cipher.AEAD
	keyID string
	rand  io.Reader
}

// NewSealer derives the credential key with HKDF-SHA256 (info "tribelt-integrations-v1").
func NewSealer(secret string) (*Sealer, error) {
	if len(secret) < 32 {
		return nil, ErrShortKey
	}
	key, err := hkdf.Key(sha256.New, []byte(secret), nil, hkdfInfo, 32)
	if err != nil {
		return nil, err
	}
	// The key id is derived separately, so it says nothing about the key itself.
	id, err := hkdf.Key(sha256.New, []byte(secret), nil, hkdfInfo+" key-id", 4)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Sealer{aead: aead, keyID: "v1-" + hex.EncodeToString(id), rand: rand.Reader}, nil
}

// KeyID names the key a credential was sealed under.
func (s *Sealer) KeyID() string { return s.keyID }

func aad(kind Kind) []byte { return []byte("tribelt-integration:" + string(kind)) }

// Seal encrypts plain for one integration kind; the kind is bound in, so a blob cannot move rows.
func (s *Sealer) Seal(kind Kind, plain []byte) ([]byte, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := io.ReadFull(s.rand, nonce); err != nil {
		return nil, err
	}
	return s.aead.Seal(nonce, nonce, plain, aad(kind)), nil
}

// Open decrypts a blob sealed for kind under keyID.
func (s *Sealer) Open(kind Kind, keyID string, blob []byte) ([]byte, error) {
	if keyID != s.keyID {
		return nil, ErrKeyChanged
	}
	n := s.aead.NonceSize()
	if len(blob) < n {
		return nil, ErrTampered
	}
	plain, err := s.aead.Open(nil, blob[:n], blob[n:], aad(kind))
	if err != nil {
		return nil, ErrTampered
	}
	return plain, nil
}

// Fingerprint is the first 12 hex characters of the credential's SHA-256: enough to tell two apart,
// never enough to recover one.
func Fingerprint(secret []byte) string {
	sum := sha256.Sum256(secret)
	return hex.EncodeToString(sum[:])[:12]
}
