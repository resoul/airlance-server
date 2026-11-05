#!/usr/bin/env bash
# Writes deploy/.env once: a database password and the settings. Safe to repeat: it never changes a value, and only adds
# the settings that a file written by an older version lacks.
set -euo pipefail
cd "$(dirname "$0")/.."

umask 077

if [[ ! -e deploy/.env ]]; then
	password=$(openssl rand -hex 32)
	wireauth_seed=$(openssl rand -base64 32)
	cat >deploy/.env <<ENV
AIRLANCE_SERVER_POSTGRES_PASSWORD=$password
AIRLANCE_SERVER_DATABASE_URL=postgres://airlance:$password@airlance-server-postgres:5432/airlance?sslmode=disable
AIRLANCE_SERVER_OIDC_ISSUER=https://auth.airlance.org
AIRLANCE_SERVER_OIDC_AUDIENCE=messenger-api
AIRLANCE_SERVER_OIDC_JWKS_URL=http://airlance-auth:8080/oauth2/jwks
AIRLANCE_SERVER_WIREAUTH_KEYS=1:$wireauth_seed
ENV
	echo "wrote deploy/.env"
fi

if ! grep -q '^AIRLANCE_SERVER_S3_ACCESS_KEY=' deploy/.env; then
	access_key=$(openssl rand -hex 16)
	secret_key=$(openssl rand -hex 32)
	cat >>deploy/.env <<ENV
AIRLANCE_SERVER_S3_ENDPOINT=http://airlance-server-files:8333
AIRLANCE_SERVER_S3_PUBLIC_URL=https://files.airlance.org
AIRLANCE_SERVER_S3_ACCESS_KEY=$access_key
AIRLANCE_SERVER_S3_SECRET_KEY=$secret_key
AIRLANCE_SERVER_S3_BUCKET=media
ENV
	echo "added the file storage settings to deploy/.env"
fi

if ! grep -q '^AIRLANCE_SERVER_MEILI_KEY=' deploy/.env; then
	meili_key=$(openssl rand -hex 32)
	cat >>deploy/.env <<ENV
AIRLANCE_SERVER_MEILI_URL=http://airlance-server-meilisearch:7700
AIRLANCE_SERVER_MEILI_KEY=$meili_key
ENV
	echo "added the search settings to deploy/.env"
fi
