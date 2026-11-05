# Wire Handshake Protocol v3

The encrypted channel between the app and the server, on top of TLS. TLS ends at Cloudflare; this ends at our server, so
Cloudflare carries tokens and frames it cannot read. It is transport security, not end-to-end encryption between people: the
server sees the content.

This file is the single description of the protocol. The copy in `airlance-server/internal/transport/wireauth/SPEC.md` is
refreshed by `make schema` in the server. Integers are little-endian unless stated otherwise. All messages are binary WebSocket
frames.

## Keys

The server holds Ed25519 signing keys, each with a `u32` id. The app pins their public keys, newest first, at most 8.

To rotate: add the new key to the server, ship an app that pins both, wait until old apps are gone, remove the old key.

## Handshake

| Direction | Message | Layout |
| --- | --- | --- |
| Client → server | `hello` | cmd `201` (u32), `client_nonce` (16), `count` (u8, 1 to 8), `key_id` (u32) × count |
| Server → client | `server_hello` | `server_nonce` (16), `key_id` (u32): the first offered key the server holds |
| Client → server | `exchange` | cmd `202` (u32), `client_pubkey` (65: P-256, uncompressed) |
| Server → client | `server_key` | `server_pubkey` (65), `signature` (64) |

```text
transcript = SHA256("airlance-wireauth/v3/transcript" ‖ hello ‖ server_hello ‖ exchange ‖ server_pubkey)
signature  = Ed25519(key_id's private key, "airlance-wireauth/v3/signature" ‖ transcript)
```

The transcript covers every byte of all four messages except the signature, so changing any of them, including the list of
offered keys, breaks the signature. The client must verify it against the key pinned for `key_id` before it derives anything. On
failure it closes the connection.

## Traffic keys

```text
shared   = ECDH(own_private, peer_public)
salt     = client_nonce ‖ server_nonce
c2s_key  = HKDF-SHA256(shared, salt, "airlance-wireauth/v3/client-to-server" ‖ transcript, 32)
s2c_key  = HKDF-SHA256(shared, salt, "airlance-wireauth/v3/server-to-client" ‖ transcript, 32)
```

A packet of one direction cannot be accepted in the other. The ECDH keys are new on every connection, so a stolen signing key
does not reveal earlier traffic.

## Channel

Every message after the handshake:

| Offset | Size | Field | Note |
| --- | --- | --- | --- |
| 0 | 8 | `seq` | u64 **big-endian**, starts at 0 in each direction, grows by 1 |
| 8 | N | ciphertext | AES-256-GCM |
| 8+N | 16 | tag | |

The GCM nonce is 4 zero bytes followed by `seq`, and the additional data is `seq`. Each key lives for one connection, so a nonce
never repeats. A receiver accepts only the next expected `seq`. Anything else, including a replay, is refused before decryption,
and the server closes the connection.

## After the handshake

The plaintext of each packet is one FlatBuffers envelope (`submodules/Protocol`). The first one must be `Authenticate` with the
access token. The server answers `Authenticated`, or `Error` 401 and close 4401 for a bad token, or `Error` 503 and close 1013
when the check is unavailable. The token never travels outside the channel.
