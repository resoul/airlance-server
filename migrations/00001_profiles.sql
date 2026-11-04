-- +goose Up
CREATE TABLE profiles (
    subject    text PRIMARY KEY,
    first_name text NOT NULL CHECK (first_name <> ''),
    last_name  text NOT NULL DEFAULT '',
    username   text,
    bio        text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL
);

CREATE UNIQUE INDEX profiles_username ON profiles (lower(username)) WHERE username IS NOT NULL;

-- +goose Down
DROP TABLE profiles;
