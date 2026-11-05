-- +goose Up
CREATE SEQUENCE search_queue_version;

CREATE TABLE search_queue (
    subject text PRIMARY KEY,
    version bigint NOT NULL DEFAULT nextval('search_queue_version')
);

INSERT INTO search_queue (subject) SELECT subject FROM profiles;

-- +goose Down
DROP TABLE search_queue;
DROP SEQUENCE search_queue_version;
