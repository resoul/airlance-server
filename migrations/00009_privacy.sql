-- +goose Up
CREATE TABLE profile_privacy (
    subject  text PRIMARY KEY,
    phone    smallint NOT NULL DEFAULT 2,
    photos   smallint NOT NULL DEFAULT 0,
    bio      smallint NOT NULL DEFAULT 0,
    messages smallint NOT NULL DEFAULT 0,
    search   smallint NOT NULL DEFAULT 0
);

CREATE TABLE blocks (
    owner      text NOT NULL,
    blocked    text NOT NULL,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (owner, blocked)
);

CREATE INDEX blocks_blocked ON blocks (blocked);

-- +goose Down
DROP TABLE blocks;
DROP TABLE profile_privacy;
