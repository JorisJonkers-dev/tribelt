package hits

import (
	"context"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5"

	"github.com/JorisJonkers-dev/tribelt/internal/platform/pg/queries"
)

// TxBeginner is the part of a pgx pool the writer needs.
type TxBeginner interface {
	queries.DBTX
	Begin(ctx context.Context) (pgx.Tx, error)
}

// PGWriter writes a batch in one transaction and falls back to one statement at a time when the
// batch fails, so one bad row never loses the others.
type PGWriter struct {
	DB  TxBeginner
	Log *slog.Logger
}

// Write implements Writer.
func (w PGWriter) Write(ctx context.Context, ops []Op) ([]Op, error) {
	unmatched, err := w.batch(ctx, ops)
	if err == nil {
		return unmatched, nil
	}
	w.Log.Warn("hit batch failed, writing one by one", "ops", len(ops), "error", err)
	unmatched = unmatched[:0]
	q := queries.New(w.DB)
	var errs []error
	for _, op := range ops {
		matched, err := apply(ctx, q, op)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !matched {
			unmatched = append(unmatched, op)
		}
	}
	if len(errs) == len(ops) {
		return nil, errors.Join(errs...)
	}
	for _, e := range errs {
		w.Log.Error("hit op dropped", "error", e)
	}
	return unmatched, nil
}

func (w PGWriter) batch(ctx context.Context, ops []Op) ([]Op, error) {
	tx, err := w.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := queries.New(tx)
	var unmatched []Op
	for _, op := range ops {
		matched, err := apply(ctx, q, op)
		if err != nil {
			return nil, err
		}
		if !matched {
			unmatched = append(unmatched, op)
		}
	}
	return unmatched, tx.Commit(ctx)
}

// apply runs one Op; matched is false only for a beacon that found no Hit.
func apply(ctx context.Context, q *queries.Queries, op Op) (matched bool, err error) {
	switch {
	case op.Hit != nil:
		return true, q.InsertHit(ctx, *op.Hit)
	case op.Beacon != nil:
		n, err := q.ConfirmBeacon(ctx, *op.Beacon)
		return n > 0, err
	case op.Click != nil:
		return true, q.InsertOutboundClick(ctx, *op.Click)
	}
	return true, nil
}
