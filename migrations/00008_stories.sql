-- +goose Up
CREATE TABLE stories (
    id             text PRIMARY KEY,
    owner          text NOT NULL,
    client_id      text NOT NULL,
    media_id       text NOT NULL,
    cover_media_id text NOT NULL DEFAULT '',
    is_video       boolean NOT NULL,
    meta           text NOT NULL DEFAULT '',
    created_at     timestamptz NOT NULL,
    expires_at     timestamptz NOT NULL,
    UNIQUE (owner, client_id)
);

CREATE INDEX stories_owner_live ON stories (owner, expires_at);
CREATE INDEX stories_expiry ON stories (expires_at);
CREATE INDEX stories_media ON stories (media_id);
CREATE INDEX stories_cover ON stories (cover_media_id) WHERE cover_media_id <> '';

CREATE TABLE story_views (
    story_id  text NOT NULL,
    viewer    text NOT NULL,
    viewed_at timestamptz NOT NULL,
    PRIMARY KEY (story_id, viewer)
);

-- +goose Down
DROP TABLE story_views;
DROP TABLE stories;
