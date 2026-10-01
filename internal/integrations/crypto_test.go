package integrations

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

const (
	testKey  = "a-session-key-of-at-least-32-characters"
	otherKey = "another-session-key-of-32-characters!!"
)

func sealer(t *testing.T, secret string) *Sealer {
	t.Helper()
	s, err := NewSealer(secret)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSealRoundTrip(t *testing.T) {
	s := sealer(t, testKey)
	plain := []byte(`{"type":"service_account"}`)
	a, err := s.Seal(Google, plain)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := s.Seal(Google, plain)
	if bytes.Equal(a, b) || bytes.Contains(a, plain) {
		t.Fatal("every seal uses a fresh nonce and hides the plaintext")
	}
	got, err := s.Open(Google, s.KeyID(), a)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("round trip: %q %v", got, err)
	}
	if !strings.HasPrefix(s.KeyID(), "v1-") || s.KeyID() != sealer(t, testKey).KeyID() {
		t.Fatalf("key id is stable per SESSION_KEY: %s", s.KeyID())
	}
}

func TestSealTamperDetection(t *testing.T) {
	s := sealer(t, testKey)
	blob, _ := s.Seal(Bing, []byte("bing-api-key-value"))
	for name, mutate := range map[string]func([]byte) []byte{
		"flipped ciphertext": func(b []byte) []byte { b[len(b)-1] ^= 1; return b },
		"flipped nonce":      func(b []byte) []byte { b[0] ^= 1; return b },
		"truncated":          func(b []byte) []byte { return b[:5] },
		"empty":              func([]byte) []byte { return nil },
	} {
		if _, err := s.Open(Bing, s.KeyID(), mutate(bytes.Clone(blob))); !errors.Is(err, ErrTampered) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := s.Open(Cloudflare, s.KeyID(), blob); !errors.Is(err, ErrTampered) {
		t.Fatalf("a blob cannot move to another kind: %v", err)
	}
}

func TestSealWrongKey(t *testing.T) {
	s, other := sealer(t, testKey), sealer(t, otherKey)
	blob, _ := s.Seal(Bing, []byte("bing-api-key-value"))
	if other.KeyID() == s.KeyID() {
		t.Fatal("another SESSION_KEY has another key id")
	}
	if _, err := other.Open(Bing, s.KeyID(), blob); !errors.Is(err, ErrKeyChanged) || CodeOf(err) != CodeKeyChanged {
		t.Fatalf("rotation is detected by key id: %v", err)
	}
	if _, err := other.Open(Bing, other.KeyID(), blob); !errors.Is(err, ErrTampered) {
		t.Fatalf("the wrong key never opens a blob: %v", err)
	}
	if _, err := NewSealer("short"); !errors.Is(err, ErrShortKey) {
		t.Fatal("short keys are refused")
	}
	if _, err := s.Seal(Bing, nil); err != nil {
		t.Fatal(err)
	}
	s.rand = bytes.NewReader(nil)
	if _, err := s.Seal(Bing, []byte("x")); err == nil {
		t.Fatal("no randomness, no seal")
	}
}

func TestFingerprint(t *testing.T) {
	f := Fingerprint([]byte("secret"))
	if len(f) != 12 || f != Fingerprint([]byte("secret")) || f == Fingerprint([]byte("secret2")) || strings.Contains(f, "secret") {
		t.Fatalf("fingerprint %q", f)
	}
}
