#!/bin/sh
set -e
cd "$(dirname "$0")/.."
. ./.env
dc() { docker compose --env-file .env -f deploy/docker-compose.yml "$@"; }
dc exec -T postgres dropdb -U "$POSTGRES_USER" --if-exists spincycle_test
dc exec -T postgres createdb -U "$POSTGRES_USER" spincycle_test
for f in storage/migrations/*.sql; do
  dc exec -T postgres psql -q -U "$POSTGRES_USER" -d spincycle_test -v ON_ERROR_STOP=1 < "$f"
done
echo "export TEST_DATABASE_URL=postgres://$POSTGRES_USER:$POSTGRES_PASSWORD@localhost:5432/spincycle_test"
