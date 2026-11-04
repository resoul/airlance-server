# airlance-server

The server of the Airlance messenger. Go, PostgreSQL, a WebSocket that carries FlatBuffers. People sign in at
[airlance-auth](https://github.com/resoul/auth); this server only checks the tokens it issues.

The first slice is one person's own profile. Its purpose is to prove the whole chain on something simple: a token, a
connection, a use case, a row in the database.

## Layers

```text
transport ─────┐                      cmd/airlance-server
repository ────┼──> usecase ──> domain      │
infrastructure ┘                        bootstrap (connects them all)
```

| Layer | Path | What it does | May import |
| --- | --- | --- | --- |
| Domain | `internal/domain` | Entities and rules. No SQL, no JSON, no HTTP, no tokens | nothing of ours |
| Use case | `internal/usecase` | What the product does. Declares the ports it needs (`Repository`, `Verifier`) | domain |
| Repository | `internal/repository` | Keeps the domain's data: implements the repository ports | domain, usecase |
| Infrastructure | `internal/infrastructure` | Config, database connection and migrations, the token verifier | domain, usecase |
| Transport | `internal/transport` | WebSocket, FlatBuffers (`codec`, generated `wire`), HTTP routes. Declares what it needs of a use case | domain, usecase |
| Bootstrap | `internal/bootstrap` | The one place that knows every layer | all of them |

Dependencies point inward, and `internal/architecture_test.go` fails the build if they do not. Tests are exempt: an integration
test of a repository may open a real database.

The code has no doc comments. Explanations live here.

To add a feature: rules in `domain`, the use case and its port in `usecase`, the implementation in `repository`, a kind in
the schema and a branch in `transport/ws/dispatch.go`.

## Protocol

`GET /v1/ws` with `Authorization: Bearer <access token>`. The token is checked before the connection opens: `401` means get
a new one, `503` means try later. Each binary message is one envelope, and the reply carries the same `request_id`.

| Request `kind` | Reply | Errors (`Error.code`, `Error.message`) |
| --- | --- | --- |
| `Ping` (1) | `Pong` (2) | |
| `ProfileGet` (4) | `ProfileData` (6) | `404 profile_not_found`, `403 forbidden` |
| `ProfileSet` (5) | `ProfileData` (6) | `422 invalid_name`, `422 invalid_username`, `422 invalid_bio`, `409 username_taken`, `403 forbidden` |

An unknown kind gets `400`, and a server fault gets `500 internal` with no detail. A frame that is not an envelope closes the
connection with 1007. A connection lives as long as its token: it closes with **4401** when the token expires (access tokens
last 5 minutes), and with 1001 when the server stops. The client reconnects with a fresh token.

Cancelling a read would tear the connection down without a close frame, so reads are never cancelled: when the token or the
server ends, a watcher closes the connection with the right code, and that wakes the read.

The schemas belong to the app (`Airlance/submodules/Protocol/Schema`) and `schema/` is a copy: `make schema` refreshes it and
regenerates `internal/transport/wire`. They only ever grow: new fields at the end of a table, new kinds with new numbers.

FlatBuffers for Go has no verifier and panics on an out-of-range read of a broken buffer, so `codec` reads untrusted bytes
under `recover`, inside a size limit, and copies what it needs. `TestDamagedBuffersNeverPanic` throws damaged frames at it.

## Tokens

An access token is accepted when it is RS256 with type `at+jwt`, signed by a published key of the issuer, for exactly this
API's audience and within its time. A token naming its own key (`jku`, `jwk`, `x5u`, `x5c`) is refused, and so is a service
identity. The check is offline: a revoked grant is honoured until the token expires, at most 5 minutes. A person is
identified by `sub`, never by email. Rights are the scopes `profile:read` and `profile:write`.

## Running

Register the API at auth once, from the `auth` directory:

```sh
docker compose -f deploy/compose.prod.yaml exec airlance-auth /airlance-auth oidc resource register messenger-api \
    --name Messenger --uri https://api.airlance.org --audience messenger-api \
    --scope profile:read --scope profile:write
docker compose -f deploy/compose.prod.yaml exec airlance-auth /airlance-auth oidc resource allow messenger-api \
    --client airlance-ios --scope profile:read --scope profile:write
```

Start the server, from this directory, with the auth stack already running:

```sh
scripts/init.sh
docker compose -f deploy/compose.yaml up -d --build
```

In Cloudflare, add the route `api.airlance.org` -> `http://airlance-server:8080` to the existing tunnel.

## Development

```sh
make check                                                # vet, unit tests, the layer rules
AIRLANCE_SERVER_TEST_DATABASE_URL=postgres://... make test-int
```

## Not done yet

- The app does not talk to this server: it must ask for the token with `resource=https://api.airlance.org` and gain Network
  and Sync. Until then a token of the current app is refused here, because its audience is not this API.
- Chats and messages. `seq` of the envelope stays 0 until there are updates to order.
- Limits: frames per connection, connections per person. A connection handles one request at a time.
