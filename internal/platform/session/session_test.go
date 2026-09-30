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

func TestSealOpen(t *testing.T) {
	c, err := NewCodec(strings.Repeat("k", 64))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0)
	sealed, err := c.Seal("a", payload{Sub: "u1"}, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	var got payload
	if err := c.Open("a", sealed, now.Add(59*time.Minute), &got); err != nil || got.Sub != "u1" {
		t.Fatalf("open: %v %+v", err, got)
	}
	cases := map[string]func() error{
		"expired":      func() error { return c.Open("a", sealed, now.Add(time.Hour), &got) },
		"other cookie": func() error { return c.Open("b", sealed, now, &got) },
		"tampered":     func() error { return c.Open("a", sealed[:len(sealed)-2]+"AA", now, &got) },
		"not base64":   func() error { return c.Open("a", "!!!", now, &got) },
		"too short":    func() error { return c.Open("a", "AAAA", now, &got) },
		"wrong shape":  func() error { return c.Open("a", sealed, now, &[]int{}) },
	}
	for name, fn := range cases {
		if err := fn(); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	other, _ := NewCodec(strings.Repeat("x", 64))
	if err := other.Open("a", sealed, now, &got); !errors.Is(err, ErrInvalid) {
		t.Fatal("other key must not open")
	}
}

func TestSealErrors(t *testing.T) {
	if _, err := NewCodec("short"); err == nil {
		t.Fatal("short key must fail")
	}
	c, _ := NewCodec(strings.Repeat("k", 32))
	if _, err := c.Seal("a", make(chan int), time.Now(), time.Hour); err == nil {
		t.Fatal("unmarshalable value must fail")
	}
	c.rand = strings.NewReader("")
	if _, err := c.Seal("a", 1, time.Now(), time.Hour); err == nil {
		t.Fatal("rand failure must surface")
	}
}
