-- +goose Up
CREATE TABLE chats (
    id              text PRIMARY KEY,
    direct_key      text UNIQUE,
    last_message_id bigint,
    created_at      timestamptz NOT NULL
);

CREATE TABLE chat_members (
    chat_id text NOT NULL REFERENCES chats (id),
    subject text NOT NULL,
    PRIMARY KEY (chat_id, subject)
);

CREATE INDEX chat_members_subject ON chat_members (subject);

CREATE TABLE messages (
    chat_id    text NOT NULL,
    message_id bigint NOT NULL,
    sender     text NOT NULL,
    client_id  text NOT NULL,
    text       text NOT NULL,
    sent_at    timestamptz NOT NULL,
    PRIMARY KEY (chat_id, message_id),
    UNIQUE (chat_id, sender, client_id)
) PARTITION BY HASH (chat_id);

-- +goose StatementBegin
DO $$
BEGIN
    FOR part IN 0..15 LOOP
        EXECUTE format(
            'CREATE TABLE messages_%s PARTITION OF messages FOR VALUES WITH (MODULUS 16, REMAINDER %s)', part, part
        );
    END LOOP;
END $$;
-- +goose StatementEnd

ALTER TABLE updates
    ADD COLUMN chat_id text,
    ADD COLUMN message_id bigint;

-- +goose Down
ALTER TABLE updates
    DROP COLUMN message_id,
    DROP COLUMN chat_id;
DROP TABLE messages;
DROP TABLE chat_members;
DROP TABLE chats;
