#!/usr/bin/env bash
# Writes deploy/.env once: a database password and the settings. It refuses to overwrite one.
set -euo pipefail
cd "$(dirname "$0")/.."

if [[ -e deploy/.env ]]; then
	echo "deploy/.env already exists" >&2
	exit 1
fi

password=$(openssl rand -hex 32)
umask 077
cat >deploy/.env <<ENV
AIRLANCE_SERVER_POSTGRES_PASSWORD=$password
AIRLANCE_SERVER_DATABASE_URL=postgres://airlance:$password@airlance-server-postgres:5432/airlance?sslmode=disable
AIRLANCE_SERVER_OIDC_ISSUER=https://auth.airlance.org
AIRLANCE_SERVER_OIDC_AUDIENCE=messenger-api
AIRLANCE_SERVER_OIDC_JWKS_URL=http://airlance-auth:8080/oauth2/jwks
ENV
echo "wrote deploy/.env"
