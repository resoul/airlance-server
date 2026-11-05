# airlance-server

The server of the Airlance messenger, written in Go with PostgreSQL. The iOS app connects over a WebSocket, runs an encrypted
handshake on top of TLS, and exchanges FlatBuffers messages. People sign in at [airlance-auth](https://github.com/resoul/auth); this
server only checks the tokens it issues.

It serves profiles, direct chats, groups and channels, attachments, stories, contacts, privacy, links, scheduled messages, agents, folders, pinned and muted chats. A few jobs run in the background: search indexing, cleaning up stories, sending scheduled
messages and running agents. Details are in `docs/server` of the main repository.

## Development

```sh
make check                                                # vet, unit tests, the layer rules
AIRLANCE_SERVER_TEST_DATABASE_URL=postgres://... AIRLANCE_SERVER_TEST_S3_ENDPOINT=http://... make test-int
```

## Not done yet

- Replies, formatting, reactions, read receipts and deleting or editing messages on the server.
- Locations, contacts and polls in messages.
- Push notifications and typing status.
- Archive, which still lives in the app's memory.
- A connection handles one request at a time.
