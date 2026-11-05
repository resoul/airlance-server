CREATE TABLE profiles (
    subject        text PRIMARY KEY,
    first_name     text NOT NULL CHECK (first_name <> ''),
    last_name      text NOT NULL DEFAULT '',
    username       text,
    bio            text NOT NULL DEFAULT '',
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL,
    phone          text NOT NULL DEFAULT '',
    photo_media_id text NOT NULL DEFAULT '',
    video_media_id text NOT NULL DEFAULT '',
    phone_digits   text NOT NULL DEFAULT ''
);

CREATE UNIQUE INDEX profiles_username ON profiles (lower(username)) WHERE username IS NOT NULL;
CREATE INDEX profiles_phone_digits ON profiles (phone_digits) WHERE phone_digits <> '';

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

CREATE TABLE contacts (
    owner             text NOT NULL,
    contact           text NOT NULL,
    custom_first_name text NOT NULL DEFAULT '',
    custom_last_name  text NOT NULL DEFAULT '',
    from_device       boolean NOT NULL DEFAULT false,
    added_at          timestamptz NOT NULL,
    PRIMARY KEY (owner, contact)
);

CREATE INDEX contacts_contact ON contacts (contact);

CREATE TABLE update_heads (
    subject text PRIMARY KEY,
    seq     bigint NOT NULL
);

CREATE TABLE updates (
    subject    text NOT NULL,
    seq        bigint NOT NULL,
    kind       smallint NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    chat_id    text,
    message_id bigint,
    PRIMARY KEY (subject, seq)
);

CREATE TABLE media_objects (
    id         text PRIMARY KEY,
    owner      text NOT NULL,
    size       bigint NOT NULL CHECK (size > 0),
    created_at timestamptz NOT NULL
);

CREATE INDEX media_objects_owner ON media_objects (owner);

CREATE SEQUENCE search_queue_version;

CREATE TABLE search_queue (
    subject text PRIMARY KEY,
    version bigint NOT NULL DEFAULT nextval('search_queue_version')
);

CREATE TABLE chats (
    id              text PRIMARY KEY,
    direct_key      text UNIQUE,
    last_message_id bigint,
    created_at      timestamptz NOT NULL,
    kind            smallint NOT NULL DEFAULT 0,
    title           text NOT NULL DEFAULT '',
    about           text NOT NULL DEFAULT '',
    link            text UNIQUE,
    invite_token    text UNIQUE,
    member_count    integer NOT NULL DEFAULT 2,
    created_by      text,
    client_id       text,
    CONSTRAINT chats_client UNIQUE (created_by, client_id)
);

CREATE INDEX chats_public_title ON chats (lower(title)) WHERE link IS NOT NULL;

CREATE TABLE chat_members (
    chat_id   text NOT NULL REFERENCES chats (id),
    subject   text NOT NULL,
    role      smallint NOT NULL DEFAULT 0,
    joined_at timestamptz NOT NULL DEFAULT now(),
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
    agent_id     text NOT NULL DEFAULT '',
    agent_name   text NOT NULL DEFAULT '',
    attachments  jsonb NOT NULL DEFAULT '[]',
    forward_name text NOT NULL DEFAULT '',
    PRIMARY KEY (chat_id, message_id),
    UNIQUE (chat_id, sender, client_id)
) PARTITION BY HASH (chat_id);

DO $$
BEGIN
    FOR part IN 0..15 LOOP
        EXECUTE format(
            'CREATE TABLE messages_%s PARTITION OF messages FOR VALUES WITH (MODULUS 16, REMAINDER %s)', part, part
        );
    END LOOP;
END $$;

CREATE TABLE stories (
    id             text PRIMARY KEY,
    owner          text NOT NULL,
    client_id      text NOT NULL,
    media_id       text NOT NULL,
    cover_media_id text NOT NULL DEFAULT '',
    is_video       boolean NOT NULL,
    meta           text NOT NULL DEFAULT '',
    created_at     timestamptz NOT NULL,
    expires_at     timestamptz NOT NULL,
    UNIQUE (owner, client_id)
);

CREATE INDEX stories_owner_live ON stories (owner, expires_at);
CREATE INDEX stories_expiry ON stories (expires_at);
CREATE INDEX stories_media ON stories (media_id);
CREATE INDEX stories_cover ON stories (cover_media_id) WHERE cover_media_id <> '';

CREATE TABLE story_views (
    story_id  text NOT NULL,
    viewer    text NOT NULL,
    viewed_at timestamptz NOT NULL,
    PRIMARY KEY (story_id, viewer)
);

CREATE TABLE scheduled_messages (
    id           text PRIMARY KEY,
    chat_id      text NOT NULL REFERENCES chats (id) ON DELETE CASCADE,
    sender       text NOT NULL,
    client_id    text NOT NULL,
    text         text NOT NULL,
    send_at      timestamptz NOT NULL,
    repeat_rule  smallint NOT NULL DEFAULT 0,
    timezone     text NOT NULL DEFAULT 'UTC',
    failure      text NOT NULL DEFAULT '',
    leased_until timestamptz,
    created_at   timestamptz NOT NULL,
    UNIQUE (sender, client_id)
);

CREATE INDEX scheduled_messages_due ON scheduled_messages (send_at) WHERE failure = '';
CREATE INDEX scheduled_messages_chat ON scheduled_messages (chat_id);

CREATE TABLE agents (
    id           text PRIMARY KEY,
    owner        text NOT NULL,
    client_id    text NOT NULL,
    name         text NOT NULL,
    chat_id      text NOT NULL REFERENCES chats (id) ON DELETE CASCADE,
    trigger_kind smallint NOT NULL,
    keyword      text NOT NULL DEFAULT '',
    hour         smallint NOT NULL DEFAULT 0,
    minute       smallint NOT NULL DEFAULT 0,
    weekdays     smallint NOT NULL DEFAULT 127,
    timezone     text NOT NULL DEFAULT 'UTC',
    text         text NOT NULL DEFAULT '',
    webhook_hash text UNIQUE,
    enabled      boolean NOT NULL DEFAULT true,
    next_run_at  timestamptz,
    leased_until timestamptz,
    created_at   timestamptz NOT NULL,
    updated_at   timestamptz NOT NULL,
    UNIQUE (owner, client_id)
);

CREATE INDEX agents_owner ON agents (owner);
CREATE INDEX agents_chat ON agents (chat_id, trigger_kind) WHERE enabled;
CREATE INDEX agents_due ON agents (next_run_at) WHERE enabled AND next_run_at IS NOT NULL;

CREATE TABLE agent_events (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    trigger_kind smallint NOT NULL,
    chat_id      text NOT NULL REFERENCES chats (id) ON DELETE CASCADE,
    agent_id     text REFERENCES agents (id) ON DELETE CASCADE,
    subject      text NOT NULL DEFAULT '',
    text         text NOT NULL DEFAULT '',
    leased_until timestamptz,
    created_at   timestamptz NOT NULL
);

CREATE INDEX agent_events_agent ON agent_events (agent_id) WHERE agent_id IS NOT NULL;

CREATE TABLE agent_runs (
    id       bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    agent_id text NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    status   smallint NOT NULL,
    detail   text NOT NULL DEFAULT '',
    ran_at   timestamptz NOT NULL
);

CREATE INDEX agent_runs_agent ON agent_runs (agent_id, id DESC);

CREATE TABLE message_media (
    media_id text NOT NULL,
    chat_id  text NOT NULL REFERENCES chats (id) ON DELETE CASCADE,
    PRIMARY KEY (media_id, chat_id)
);

CREATE INDEX message_media_chat ON message_media (chat_id);

CREATE TABLE chat_folder_lists (
    owner              text PRIMARY KEY,
    folders            jsonb NOT NULL,
    all_chats_position integer NOT NULL,
    shows_tags         boolean NOT NULL,
    revision           bigint NOT NULL,
    updated_at         timestamptz NOT NULL
);
