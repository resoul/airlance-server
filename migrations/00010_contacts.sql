-- +goose Up
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

ALTER TABLE profiles ADD COLUMN phone_digits text NOT NULL DEFAULT '';

UPDATE profiles SET phone_digits = regexp_replace(phone, '[^0-9]', '', 'g');

CREATE INDEX profiles_phone_digits ON profiles (phone_digits) WHERE phone_digits <> '';

-- +goose Down
DROP INDEX profiles_phone_digits;

ALTER TABLE profiles DROP COLUMN phone_digits;

DROP TABLE contacts;
