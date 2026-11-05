-- +goose Up
CREATE TABLE update_heads (
    subject text PRIMARY KEY,
    seq     bigint NOT NULL
);

CREATE TABLE updates (
    subject    text NOT NULL,
    seq        bigint NOT NULL,
    kind       smallint NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (subject, seq)
);

-- +goose Down
DROP TABLE updates;
DROP TABLE update_heads;
