package session

import (
	"errors"
	"strings"
	"testing"
	"time"
)

type payload struct {
	Sub string `json:"sub"`
}

// sealedFor is a codec and a value sealed with it for cookie "a" at now, for an hour.
func sealedFor(t *testing.T, now time.Time) (*Codec, string) {
	t.Helper()
	c, err := NewCodec(strings.Repeat("k", 64))
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := c.Seal("a", payload{Sub: "u1"}, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return c, sealed
}

var sealedAt = time.Unix(1_800_000_000, 0)

func TestASealedValueOpensWithinItsLifetime(t *testing.T) {
	c, sealed := sealedFor(t, sealedAt)
	var got payload
	if err := c.Open("a", sealed, sealedAt.Add(59*time.Minute), &got); err != nil || got.Sub != "u1" {
		t.Fatalf("open: %v %+v", err, got)
	}
}

func TestASealedValueThatWasChangedOrMisusedDoesNotOpen(t *testing.T) {
	cases := map[string]func(c *Codec, sealed string) error{
		"expired":      func(c *Codec, s string) error { return c.Open("a", s, sealedAt.Add(time.Hour), &payload{}) },
		"other cookie": func(c *Codec, s string) error { return c.Open("b", s, sealedAt, &payload{}) },
		"tampered":     func(c *Codec, s string) error { return c.Open("a", s[:len(s)-2]+"AA", sealedAt, &payload{}) },
		"not base64":   func(c *Codec, _ string) error { return c.Open("a", "!!!", sealedAt, &payload{}) },
		"too short":    func(c *Codec, _ string) error { return c.Open("a", "AAAA", sealedAt, &payload{}) },
		"wrong shape":  func(c *Codec, s string) error { return c.Open("a", s, sealedAt, &[]int{}) },
	}
	for name, open := range cases {
		t.Run(name, func(t *testing.T) {
			c, sealed := sealedFor(t, sealedAt)
			if err := open(c, sealed); !errors.Is(err, ErrInvalid) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestAnotherKeyCannotOpenASealedValue(t *testing.T) {
	_, sealed := sealedFor(t, sealedAt)
	other, _ := NewCodec(strings.Repeat("x", 64))
	if err := other.Open("a", sealed, sealedAt, &payload{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v", err)
	}
}

func TestAShortKeyIsRefused(t *testing.T) {
	if _, err := NewCodec("short"); err == nil {
		t.Fatal("accepted")
	}
}

func TestAValueThatCannotBeEncodedIsNotSealed(t *testing.T) {
	c, _ := NewCodec(strings.Repeat("k", 32))
	if _, err := c.Seal("a", make(chan int), time.Now(), time.Hour); err == nil {
		t.Fatal("sealed")
	}
}

func TestNothingIsSealedWithoutRandomness(t *testing.T) {
	c, _ := NewCodec(strings.Repeat("k", 32))
	c.rand = strings.NewReader("")
	if _, err := c.Seal("a", 1, time.Now(), time.Hour); err == nil {
		t.Fatal("sealed")
	}
}
