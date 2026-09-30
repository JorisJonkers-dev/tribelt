-- name: UpsertRelease :one
INSERT INTO releases (label, note, content_hash)
VALUES (@label, @note, @content_hash)
ON CONFLICT (label) DO UPDATE
SET note = excluded.note, content_hash = excluded.content_hash, last_seen_at = now()
RETURNING first_seen_at;

-- name: UpsertPage :exec
INSERT INTO pages (release_label, path, page_id, locale, type, title, description, h1, keywords, word_count, content_hash)
VALUES (@release_label, @path, @page_id, @locale, @type, @title, @description, @h1, @keywords, @word_count, @content_hash)
ON CONFLICT (release_label, path) DO UPDATE
SET page_id = excluded.page_id, locale = excluded.locale, type = excluded.type, title = excluded.title,
    description = excluded.description, h1 = excluded.h1, keywords = excluded.keywords,
    word_count = excluded.word_count, content_hash = excluded.content_hash;

-- name: PageLastMod :many
SELECT cur.path, min(r.first_seen_at)::timestamptz AS since
FROM pages AS cur
JOIN pages AS p ON p.path = cur.path AND p.content_hash = cur.content_hash
JOIN releases AS r ON r.label = p.release_label
WHERE cur.release_label = @release_label
GROUP BY cur.path;

-- name: ListReleases :many
SELECT * FROM releases ORDER BY first_seen_at, label;

-- name: ListPages :many
SELECT * FROM pages ORDER BY release_label, path;
