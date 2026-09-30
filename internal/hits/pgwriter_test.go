package hits

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/JorisJonkers-dev/tribelt/internal/platform/pg/pgtest"
	"github.com/JorisJonkers-dev/tribelt/internal/platform/pg/queries"
)

func pool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	p, err := pgxpool.New(context.Background(), pgtest.URL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p
}

func hit(kind, format string) *queries.InsertHitParams {
	return &queries.InsertHitParams{
		ID: uuid.New(), Ts: time.Now(), Path: "/", ReleaseLabel: "v1", Format: format, Status: 200,
		VisitorKind: kind, UserAgent: "ua", DailyHash: "d",
	}
}

func TestPGWriterWritesInOrder(t *testing.T) {
	db := pool(t)
	w := PGWriter{DB: db, Log: slog.New(slog.DiscardHandler)}
	h := hit("human-unconfirmed", "html")
	orphan := uuid.New()
	click := &queries.InsertOutboundClickParams{ID: uuid.New(), Ts: time.Now(), FromPath: "/", ReleaseLabel: "v1", Target: "https://www.tribelt.nl/", VisitorKind: "human", DailyHash: "d"}
	unmatched, err := w.Write(context.Background(), []Op{
		{Hit: h},
		{Beacon: &queries.ConfirmBeaconParams{ID: h.ID, EngagedMs: 1500, NotBefore: time.Now().Add(-time.Hour)}},
		{Beacon: &queries.ConfirmBeaconParams{ID: orphan, EngagedMs: 1, NotBefore: time.Now().Add(-time.Hour)}},
		{Click: click},
		{},
	})
	if err != nil || len(unmatched) != 1 || unmatched[0].Beacon.ID != orphan {
		t.Fatalf("unmatched=%v err=%v", unmatched, err)
	}
	got, err := queries.New(db).GetHit(context.Background(), h.ID)
	if err != nil || got.VisitorKind != "human" || !got.BeaconConfirmed || *got.EngagedMs != 1500 {
		t.Fatalf("beacon confirmed the hit: %+v %v", got, err)
	}
	// A later, shorter engagement never lowers the figure; a bot's hit is never confirmed human.
	bot := hit("ai-crawler", "md")
	_, err = w.Write(context.Background(), []Op{
		{Beacon: &queries.ConfirmBeaconParams{ID: h.ID, EngagedMs: 10, NotBefore: time.Now().Add(-time.Hour)}},
		{Hit: bot},
		{Beacon: &queries.ConfirmBeaconParams{ID: bot.ID, EngagedMs: 10, NotBefore: time.Now().Add(-time.Hour)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, _ = queries.New(db).GetHit(context.Background(), h.ID)
	b, _ := queries.New(db).GetHit(context.Background(), bot.ID)
	if *got.EngagedMs != 1500 || b.VisitorKind != "ai-crawler" || b.BeaconConfirmed {
		t.Fatalf("engaged=%d bot=%+v", *got.EngagedMs, b)
	}
}

func TestPGWriterFallsBackOneByOne(t *testing.T) {
	db := pool(t)
	w := PGWriter{DB: db, Log: slog.New(slog.DiscardHandler)}
	good, bad := hit("human", "html"), hit("human", "pdf")
	if _, err := w.Write(context.Background(), []Op{{Hit: bad}, {Hit: good}}); err != nil {
		t.Fatalf("one bad row must not fail the batch: %v", err)
	}
	if _, err := queries.New(db).GetHit(context.Background(), good.ID); err != nil {
		t.Fatal("good row landed")
	}
	if _, err := w.Write(context.Background(), []Op{{Hit: hit("martian", "html")}}); err == nil {
		t.Fatal("an all-bad batch reports its error")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := w.Write(cancelled, []Op{{Hit: good}}); err == nil {
		t.Fatal("begin failure surfaces")
	}
}
