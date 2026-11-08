CREATE TABLE link_previews (
    url text PRIMARY KEY,
    found boolean NOT NULL,
    site text NOT NULL DEFAULT '',
    title text NOT NULL DEFAULT '',
    summary text NOT NULL DEFAULT '',
    photo_media_id text NOT NULL DEFAULT '',
    width integer NOT NULL DEFAULT 0,
    height integer NOT NULL DEFAULT 0,
    fetched_at timestamptz NOT NULL
);

CREATE INDEX link_previews_photo_idx ON link_previews (photo_media_id) WHERE photo_media_id <> '';
