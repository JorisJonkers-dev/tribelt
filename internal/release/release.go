// Package release records the running Content Release and its pages in Postgres.
package release

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/JorisJonkers-dev/tribelt/internal/content"
	"github.com/JorisJonkers-dev/tribelt/internal/platform/pg/queries"
)

// Store is the part of the generated queries a sync needs.
type Store interface {
	UpsertRelease(ctx context.Context, arg queries.UpsertReleaseParams) (time.Time, error)
	UpsertPage(ctx context.Context, arg queries.UpsertPageParams) error
	PageLastMod(ctx context.Context, releaseLabel string) ([]queries.PageLastModRow, error)
}

// Sync upserts the release (with the App Version serving it and its tags) and a snapshot of every page,
// and returns when each page's current text was first published (the sitemap lastmod).
func Sync(ctx context.Context, s Store, c *content.Content, appVersion string) (map[string]time.Time, error) {
	label := c.Site.Release.Label
	tags := c.Site.Release.Tags
	if tags == nil {
		tags = []string{}
	}
	rel := queries.UpsertReleaseParams{Label: label, Note: c.Site.Release.Note, ContentHash: c.Hash, AppVersion: appVersion, Tags: tags}
	if _, err := s.UpsertRelease(ctx, rel); err != nil {
		return nil, fmt.Errorf("release: upsert %s: %w", label, err)
	}
	for _, p := range c.Pages {
		kw := p.Keywords
		if kw == nil {
			kw = []string{}
		}
		err := s.UpsertPage(ctx, queries.UpsertPageParams{
			ReleaseLabel: label, Path: p.Path, PageID: p.ID, Locale: p.Locale, Type: p.Type, Title: p.Title,
			Description: p.Description, H1: p.H1, Keywords: kw, WordCount: int64(content.WordCount(p.Body)), ContentHash: p.Hash(),
		})
		if err != nil {
			return nil, fmt.Errorf("release: page %s: %w", p.Path, err)
		}
	}
	rows, err := s.PageLastMod(ctx, label)
	if err != nil {
		return nil, fmt.Errorf("release: lastmod: %w", err)
	}
	out := make(map[string]time.Time, len(rows))
	for _, r := range rows {
		out[r.Path] = r.Since
	}
	return out, nil
}

// History is what Previous reads.
type History interface {
	LatestRelease(ctx context.Context) (string, error)
	ReleasePageHashes(ctx context.Context, releaseLabel string) ([]queries.ReleasePageHashesRow, error)
}

// Previous returns the release the last process served and its page hashes by path; read it before
// Sync records the running one. An empty label means there was none.
func Previous(ctx context.Context, h History) (string, map[string]string, error) {
	label, err := h.LatestRelease(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, nil
	}
	if err != nil {
		return "", nil, fmt.Errorf("release: latest: %w", err)
	}
	rows, err := h.ReleasePageHashes(ctx, label)
	if err != nil {
		return "", nil, fmt.Errorf("release: pages of %s: %w", label, err)
	}
	out := make(map[string]string, len(rows))
	for _, r := range rows {
		out[r.Path] = r.ContentHash
	}
	return label, out, nil
}

// Hashes is the content hash of every page of c by path, as Sync stores them.
func Hashes(c *content.Content) map[string]string {
	out := make(map[string]string, len(c.Pages))
	for _, p := range c.Pages {
		out[p.Path] = p.Hash()
	}
	return out
}
