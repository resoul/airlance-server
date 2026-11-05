-- +goose Up
ALTER TABLE profiles
    ADD COLUMN phone          text NOT NULL DEFAULT '',
    ADD COLUMN photo_media_id text NOT NULL DEFAULT '',
    ADD COLUMN video_media_id text NOT NULL DEFAULT '';

CREATE INDEX profiles_photo_media ON profiles (photo_media_id) WHERE photo_media_id <> '';
CREATE INDEX profiles_video_media ON profiles (video_media_id) WHERE video_media_id <> '';

-- +goose Down
DROP INDEX profiles_video_media;
DROP INDEX profiles_photo_media;

ALTER TABLE profiles
    DROP COLUMN video_media_id,
    DROP COLUMN photo_media_id,
    DROP COLUMN phone;
