ALTER TABLE chats
    ADD COLUMN no_copy boolean NOT NULL DEFAULT false,
    ADD COLUMN signs boolean NOT NULL DEFAULT false,
    ADD COLUMN reactions_mode smallint NOT NULL DEFAULT 0,
    ADD COLUMN reactions text[] NOT NULL DEFAULT '{}',
    ADD COLUMN reactions_per_post smallint NOT NULL DEFAULT 0;
