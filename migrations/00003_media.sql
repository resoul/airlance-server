-- +goose Up
CREATE TABLE media_objects (
    id         text PRIMARY KEY,
    owner      text NOT NULL,
    size       bigint NOT NULL CHECK (size > 0),
    created_at timestamptz NOT NULL
);

CREATE INDEX media_objects_owner ON media_objects (owner);

-- +goose Down
DROP TABLE media_objects;
