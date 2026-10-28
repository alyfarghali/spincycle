#!/usr/bin/env bash
# Usage: deploy/deploy.sh [service...]
set -euo pipefail

host="${SERVER:-root@2.29.125.26}"
root="$(cd "$(dirname "$0")/.." && pwd)"
dir=/srv/src/spincycle

# Excluded paths are also protected from --delete, so the server's .env link and certificates survive.
rsync -az --delete \
  --exclude .git --exclude .env --exclude '*.pem' --exclude '*.key' \
  --exclude broker/certs/prod --exclude firmware --exclude hardware \
  --exclude node_modules --exclude .venv --exclude __pycache__ \
  --exclude .vscode --exclude TODO.md \
  "$root/" "$host:$dir/"

ssh "$host" docker compose --project-directory "$dir/deploy" --env-file "$dir/.env" \
  -f "$dir/deploy/docker-compose.yml" -f "$dir/deploy/docker-compose.prod.yml" \
  up -d --build --remove-orphans "$@"
