package hits

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/JorisJonkers-dev/tribelt/internal/platform/pg/queries"
)

func hitOp() Op { return Op{Hit: &queries.InsertHitParams{ID: uuid.New()}} }

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestRecorderFlushesOnBatchAndTick(t *testing.T) {
	w := &memWriter{}
	r := NewRecorder(w, slog.New(slog.DiscardHandler), 10)
	r.batch, r.interval = 3, 20*time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()
	for range 4 {
		r.Enqueue(hitOp())
	}
	waitFor(t, func() bool { return len(w.snapshot()) == 4 })
	if r.Written() != 4 {
		t.Fatalf("written %d", r.Written())
	}
	cancel()
	<-done
}

func TestRecorderDropsWhenFull(t *testing.T) {
	w := &memWriter{}
	r := NewRecorder(w, slog.New(slog.DiscardHandler), 2)
	for range 5 {
		r.Enqueue(hitOp())
	}
	if r.Dropped() != 3 {
		t.Fatalf("dropped %d", r.Dropped())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r.Run(ctx) // drains what was buffered, then returns
	if len(w.snapshot()) != 2 {
		t.Fatalf("drained %d", len(w.snapshot()))
	}
}

func TestRecorderRetriesUnmatchedBeacons(t *testing.T) {
	w := &memWriter{unmatchedTimes: 2}
	r := NewRecorder(w, slog.New(slog.DiscardHandler), 10)
	r.interval = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()
	r.Enqueue(Op{Beacon: &queries.ConfirmBeaconParams{ID: uuid.New()}})
	waitFor(t, func() bool { return len(w.snapshot()) == 1 })
	cancel()
	<-done

	gone := &memWriter{unmatchedTimes: 100}
	r = NewRecorder(gone, slog.New(slog.DiscardHandler), 10)
	r.interval = 5 * time.Millisecond
	ctx, cancel = context.WithCancel(context.Background())
	done = make(chan struct{})
	go func() { r.Run(ctx); close(done) }()
	r.Enqueue(Op{Beacon: &queries.ConfirmBeaconParams{ID: uuid.New()}})
	waitFor(t, func() bool { gone.mu.Lock(); defer gone.mu.Unlock(); return gone.unmatchedTimes == 96 })
	time.Sleep(30 * time.Millisecond)
	cancel()
	<-done
	if gone.unmatchedTimes != 96 {
		t.Fatalf("a beacon without a hit is retried three times, then dropped (%d left)", gone.unmatchedTimes)
	}
}

func TestRecorderSurvivesWriteErrors(t *testing.T) {
	w := &memWriter{fail: errors.New("db down")}
	r := NewRecorder(w, slog.New(slog.DiscardHandler), 10)
	r.Enqueue(hitOp())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r.Run(ctx)
	if r.Written() != 0 {
		t.Fatal("nothing written")
	}
}
