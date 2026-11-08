DROP TABLE wallpapers;
DROP TABLE chat_bans;
DROP TABLE user_settings;
DROP TABLE chat_drafts;
DROP TABLE poll_votes;
DROP TABLE message_reactions;
DROP TABLE chat_pins;
DROP TABLE message_views;
DROP TABLE message_hidden;

ALTER TABLE chat_members
    DROP COLUMN is_archived,
    DROP COLUMN rights,
    DROP COLUMN delivered_up_to,
    DROP COLUMN read_up_to;

ALTER TABLE chats DROP COLUMN member_rights;

DROP INDEX messages_thread;

ALTER TABLE messages
    DROP COLUMN poll,
    DROP COLUMN views,
    DROP COLUMN thread_id,
    DROP COLUMN deleted_at,
    DROP COLUMN edited_at,
    DROP COLUMN entities,
    DROP COLUMN reply_to_id;
