CREATE TABLE chat_log (
    id bigserial PRIMARY KEY,
    chat_id text NOT NULL REFERENCES chats (id) ON DELETE CASCADE,
    actor text NOT NULL,
    kind smallint NOT NULL,
    target text NOT NULL DEFAULT '',
    detail text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX chat_log_chat_idx ON chat_log (chat_id, id DESC);
