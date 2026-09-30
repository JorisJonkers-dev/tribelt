package visits

import (
	"bytes"
	"errors"
	"net/netip"
	"testing"
	"time"
)

func TestVisitorIDRules(t *testing.T) {
	if !OptedOut("1", "") || !OptedOut("", "1") || OptedOut("0", "0") || OptedOut("", "") {
		t.Fatal("OptedOut")
	}
	for _, k := range Kinds() {
		if WantsVisitorID(k, false) != k.IsHuman() {
			t.Fatalf("WantsVisitorID(%s)", k)
		}
		if WantsVisitorID(k, true) {
			t.Fatalf("opted-out %s must not get a cookie", k)
		}
	}
}

func TestNewVisitorID(t *testing.T) {
	id, err := NewVisitorID(bytes.NewReader(make([]byte, 16)))
	if err != nil || !ValidVisitorID(id) || id != "AAAAAAAAAAAAAAAAAAAAAA" {
		t.Fatalf("id=%q err=%v", id, err)
	}
	if _, err := NewVisitorID(bytes.NewReader(nil)); err == nil {
		t.Fatal("short read must fail")
	}
	for _, bad := range []string{"", "short", "AAAAAAAAAAAAAAAAAAAAA!", "AAAAAAAAAAAAAAAAAAAAAAA"} {
		if ValidVisitorID(bad) {
			t.Fatalf("ValidVisitorID(%q)", bad)
		}
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("boom") }

func TestNewVisitorIDReaderError(t *testing.T) {
	if _, err := NewVisitorID(failingReader{}); err == nil {
		t.Fatal("reader error must surface")
	}
}

func TestDailyHashRotatesOnAmsterdamDay(t *testing.T) {
	h := NewDailyHasher([]byte("key"))
	ip := netip.MustParseAddr("203.0.113.7")
	ua := "Mozilla/5.0"
	// 21:30 UTC on 30 Sept is 23:30 in Amsterdam (CEST); 22:30 UTC is already 1 Oct there.
	before := time.Date(2026, 9, 30, 21, 30, 0, 0, time.UTC)
	sameDay := time.Date(2026, 9, 30, 5, 0, 0, 0, time.UTC)
	after := time.Date(2026, 9, 30, 22, 30, 0, 0, time.UTC)

	a := h.Hash(before, ip, ua)
	if len(a) != 32 {
		t.Fatalf("hash length %d", len(a))
	}
	if h.Hash(sameDay, ip, ua) != a {
		t.Fatal("same Amsterdam day must hash equal")
	}
	if h.Hash(after, ip, ua) == a {
		t.Fatal("next Amsterdam day must rotate")
	}
	if h.Hash(before, ip, ua+"x") == a || h.Hash(before, netip.MustParseAddr("203.0.113.8"), ua) == a {
		t.Fatal("different client must differ")
	}
	if NewDailyHasher([]byte("other")).Hash(before, ip, ua) == a {
		t.Fatal("key must matter")
	}
	if h.Day(after) != "2026-10-01" {
		t.Fatalf("Day = %s", h.Day(after))
	}
}
