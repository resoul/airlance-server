# airlance-server

The server of the Airlance messenger, written in Go with PostgreSQL. The iOS app connects over a WebSocket, runs an encrypted
handshake on top of TLS, and exchanges FlatBuffers messages. People sign in at [airlance-auth](https://github.com/resoul/auth); this
server only checks the tokens it issues.

The first slice is one person's own profile. Its purpose is to prove the whole chain on something simple: a token, a
connection, a use case, a row in the database.

## Development

```sh
make check                                                # vet, unit tests, the layer rules
AIRLANCE_SERVER_TEST_DATABASE_URL=postgres://... AIRLANCE_SERVER_TEST_S3_ENDPOINT=http://... make test-int
```

## Not done yet

- Chats and messages. The only update so far is a changed profile, and that is all the app's Sync applies.
- Files in the app: the server gives signed links to SeaweedFS, but the app does not use them yet.
- The search screen in the app: the server finds people through Meilisearch, but the app does not ask yet.
- A connection handles one request at a time.
