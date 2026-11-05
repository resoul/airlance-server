-- +goose Up
CREATE SEQUENCE profile_photos_position;

CREATE TABLE profile_photos (
    subject        text NOT NULL,
    photo_media_id text NOT NULL,
    video_media_id text NOT NULL DEFAULT '',
    position       bigint NOT NULL DEFAULT nextval('profile_photos_position'),
    added_at       timestamptz NOT NULL,
    PRIMARY KEY (subject, photo_media_id)
);

CREATE INDEX profile_photos_order ON profile_photos (subject, position DESC);
CREATE INDEX profile_photos_photo ON profile_photos (photo_media_id);
CREATE INDEX profile_photos_video ON profile_photos (video_media_id) WHERE video_media_id <> '';

INSERT INTO profile_photos (subject, photo_media_id, video_media_id, added_at)
SELECT subject, photo_media_id, video_media_id, updated_at FROM profiles WHERE photo_media_id <> '';

DROP INDEX profiles_video_media;
DROP INDEX profiles_photo_media;

-- +goose Down
CREATE INDEX profiles_photo_media ON profiles (photo_media_id) WHERE photo_media_id <> '';
CREATE INDEX profiles_video_media ON profiles (video_media_id) WHERE video_media_id <> '';

DROP TABLE profile_photos;
DROP SEQUENCE profile_photos_position;
