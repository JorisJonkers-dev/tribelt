// Package hits records Hits, beacon confirmations and Outbound Clicks without ever slowing or failing
// a page: requests enqueue, one goroutine writes batches to Postgres.
package hits

import (
	"context"
	"log/slog"
	"slices"
	"sync/atomic"
	"time"

	"github.com/JorisJonkers-dev/tribelt/internal/platform/pg/queries"
)

// Op is one pending write. Exactly one field is set.
type Op struct {
	Hit    *queries.InsertHitParams
	Beacon *queries.ConfirmBeaconParams
	Click  *queries.InsertOutboundClickParams
	// tries counts beacon confirmations that found no Hit yet.
	tries int
}

// Writer persists a batch in order and reports beacons that matched no Hit.
type Writer interface {
	Write(ctx context.Context, ops []Op) (unmatched []Op, err error)
}

// Recorder buffers Ops and flushes them from a single goroutine.
type Recorder struct {
	ch       chan Op
	w        Writer
	log      *slog.Logger
	batch    int
	interval time.Duration
	dropped  atomic.Int64
	written  atomic.Int64
}

// NewRecorder buffers up to capacity Ops; beyond that new Ops are dropped and counted.
func NewRecorder(w Writer, log *slog.Logger, capacity int) *Recorder {
	return &Recorder{ch: make(chan Op, capacity), w: w, log: log, batch: 200, interval: time.Second}
}

// Enqueue never blocks.
func (r *Recorder) Enqueue(op Op) {
	select {
	case r.ch <- op:
	default:
		if r.dropped.Add(1)%100 == 1 {
			r.log.Warn("hit buffer full, dropping", "dropped", r.dropped.Load())
		}
	}
}

// Dropped and Written are counters for logs and tests.
func (r *Recorder) Dropped() int64 { return r.dropped.Load() }

// Written counts Ops persisted.
func (r *Recorder) Written() int64 { return r.written.Load() }

// Run flushes until ctx ends, then drains what is left.
func (r *Recorder) Run(ctx context.Context) {
	tick := time.NewTicker(r.interval)
	defer tick.Stop()
	var pending, retry []Op
	flush := func(fctx context.Context) {
		if len(pending) == 0 && len(retry) == 0 {
			return
		}
		ops := slices.Concat(pending, retry)
		pending, retry = nil, nil
		wctx, cancel := context.WithTimeout(fctx, 10*time.Second)
		defer cancel()
		unmatched, err := r.w.Write(wctx, ops)
		if err != nil {
			r.log.Error("hit write failed", "ops", len(ops), "error", err)
			return
		}
		r.written.Add(int64(len(ops) - len(unmatched)))
		for _, op := range unmatched {
			if op.tries < 3 {
				op.tries++
				retry = append(retry, op)
			}
		}
	}
	for {
		select {
		case op := <-r.ch:
			pending = append(pending, op)
			if len(pending) >= r.batch {
				flush(ctx)
			}
		case <-tick.C:
			flush(ctx)
		case <-ctx.Done():
			for len(r.ch) > 0 {
				pending = append(pending, <-r.ch)
			}
			flush(context.WithoutCancel(ctx))
			return
		}
	}
}
