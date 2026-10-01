-- name: UpsertRelease :one
-- app_version is the build that first published the release; app_versions every build that served it.
INSERT INTO releases (label, note, content_hash, app_version, app_versions, tags)
VALUES (@label, @note, @content_hash, @app_version::text, ARRAY[@app_version::text], @tags::text [])
ON CONFLICT (label) DO UPDATE
SET note = excluded.note, content_hash = excluded.content_hash, tags = excluded.tags, last_seen_at = now(),
    app_version = CASE WHEN releases.app_version = '' THEN excluded.app_version ELSE releases.app_version END,
    app_versions = CASE
        WHEN excluded.app_version = ANY (releases.app_versions) THEN releases.app_versions
        ELSE releases.app_versions || excluded.app_version
    END
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

-- name: ReleaseTags :many
SELECT DISTINCT unnest(tags)::text AS tag FROM releases ORDER BY 1;

-- name: AppVersions :many
SELECT DISTINCT unnest(app_versions)::text AS app_version FROM releases ORDER BY 1;
