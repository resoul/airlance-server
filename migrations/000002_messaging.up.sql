ALTER TABLE messages
    ADD COLUMN reply_to_id bigint NOT NULL DEFAULT 0,
    ADD COLUMN entities    jsonb NOT NULL DEFAULT '[]',
    ADD COLUMN edited_at   timestamptz,
    ADD COLUMN deleted_at  timestamptz,
    ADD COLUMN thread_id   bigint NOT NULL DEFAULT 0,
    ADD COLUMN views       integer NOT NULL DEFAULT 0,
    ADD COLUMN poll        jsonb;

CREATE INDEX messages_thread ON messages (chat_id, thread_id, message_id);

ALTER TABLE chat_members
    ADD COLUMN read_up_to  bigint NOT NULL DEFAULT 0,
    ADD COLUMN delivered_up_to bigint NOT NULL DEFAULT 0,
    ADD COLUMN rights      bigint NOT NULL DEFAULT 0,
    ADD COLUMN is_archived boolean NOT NULL DEFAULT false;

ALTER TABLE chats ADD COLUMN member_rights bigint NOT NULL DEFAULT 7;

CREATE TABLE chat_bans (
    chat_id   text NOT NULL,
    subject   text NOT NULL,
    banned_by text NOT NULL,
    banned_at timestamptz NOT NULL,
    PRIMARY KEY (chat_id, subject)
);

CREATE TABLE message_hidden (
    subject    text NOT NULL,
    chat_id    text NOT NULL,
    message_id bigint NOT NULL,
    PRIMARY KEY (subject, chat_id, message_id)
);

CREATE TABLE message_views (
    chat_id    text NOT NULL,
    message_id bigint NOT NULL,
    viewer     text NOT NULL,
    PRIMARY KEY (chat_id, message_id, viewer)
);

CREATE TABLE chat_pins (
    chat_id    text NOT NULL,
    message_id bigint NOT NULL,
    pinned_at  timestamptz NOT NULL,
    PRIMARY KEY (chat_id, message_id)
);

CREATE TABLE message_reactions (
    chat_id    text NOT NULL,
    message_id bigint NOT NULL,
    subject    text NOT NULL,
    emoji      text NOT NULL,
    reacted_at timestamptz NOT NULL,
    PRIMARY KEY (chat_id, message_id, subject)
);

CREATE TABLE poll_votes (
    chat_id    text NOT NULL,
    message_id bigint NOT NULL,
    subject    text NOT NULL,
    option     smallint NOT NULL,
    voted_at   timestamptz NOT NULL,
    PRIMARY KEY (chat_id, message_id, subject, option)
);

CREATE INDEX poll_votes_message ON poll_votes (chat_id, message_id, option, voted_at DESC);

CREATE TABLE chat_drafts (
    chat_id    text NOT NULL,
    subject    text NOT NULL,
    text       text NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (chat_id, subject)
);

CREATE TABLE user_settings (
    subject    text NOT NULL,
    key        text NOT NULL,
    version    bigint NOT NULL,
    data       bytea NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (subject, key)
);

CREATE TABLE wallpapers (
    id         text PRIMARY KEY,
    name       text NOT NULL,
    media_id   text NOT NULL UNIQUE,
    sort       integer NOT NULL,
    created_at timestamptz NOT NULL
);
