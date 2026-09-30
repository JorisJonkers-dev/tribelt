// Package session seals small values into tamper-proof, encrypted cookie strings (AES-256-GCM).
package session

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"time"
)

// ErrInvalid is returned for any cookie that does not open: tampered, expired or foreign.
var ErrInvalid = errors.New("session: invalid")

// Codec seals and opens values; the cookie name is bound in as associated data.
type Codec struct {
	aead cipher.AEAD
	rand io.Reader
}

// NewCodec derives an AES-256 key from an opaque secret of at least 32 characters.
func NewCodec(secret string) (*Codec, error) {
	if len(secret) < 32 {
		return nil, errors.New("session: SESSION_KEY must be at least 32 characters")
	}
	key, err := hkdf.Key(sha256.New, []byte(secret), nil, "tribelt session cookie v1", 32)
	if err != nil {
		return nil, err
	}
	block, _ := aes.NewCipher(key)
	aead, _ := cipher.NewGCM(block)
	return &Codec{aead: aead, rand: rand.Reader}, nil
}

type envelope struct {
	Exp  int64           `json:"exp"`
	Data json.RawMessage `json:"d"`
}

// Seal encrypts v for the named cookie until now+ttl.
func (c *Codec) Seal(name string, v any, now time.Time, ttl time.Duration) (string, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	plain, _ := json.Marshal(envelope{Exp: now.Add(ttl).Unix(), Data: data})
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(c.rand, nonce); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(c.aead.Seal(nonce, nonce, plain, []byte(name))), nil
}

// Open decrypts a value sealed for the named cookie and rejects it once expired.
func (c *Codec) Open(name, sealed string, now time.Time, v any) error {
	raw, err := base64.RawURLEncoding.DecodeString(sealed)
	if err != nil || len(raw) < c.aead.NonceSize() {
		return ErrInvalid
	}
	plain, err := c.aead.Open(nil, raw[:c.aead.NonceSize()], raw[c.aead.NonceSize():], []byte(name))
	if err != nil {
		return ErrInvalid
	}
	var env envelope
	if json.Unmarshal(plain, &env) != nil || now.Unix() >= env.Exp {
		return ErrInvalid
	}
	if json.Unmarshal(env.Data, v) != nil {
		return ErrInvalid
	}
	return nil
}
