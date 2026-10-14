#!/usr/bin/env bash
# Copy the TLS cert Caddy obtained into the broker and reload Mosquitto on renewal.
#
# Production only. Run daily from cron on the server, as root:
#   0 4 * * * /srv/spincycle/broker/certs/sync-certs.sh
# Needs DOMAIN and BROKER_CERT_DIR in .env, and the proxy (Caddy) service running.
set -euo pipefail

root="$(cd "$(dirname "$0")/../.." && pwd)"
set -a
# shellcheck disable=SC1091
source "$root/.env"
set +a
: "${DOMAIN:?DOMAIN must be set in .env}"
: "${BROKER_CERT_DIR:?BROKER_CERT_DIR must be set in .env}"

compose() {
  docker compose --project-directory "$root/deploy" --env-file "$root/.env" \
    -f "$root/deploy/docker-compose.yml" "$@"
}

# Where Caddy keeps Let's Encrypt certificates inside its data volume.
src="/data/caddy/certificates/acme-v02.api.letsencrypt.org-directory/$DOMAIN"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

compose cp "proxy:$src/$DOMAIN.crt" "$tmp/fullchain.pem"
compose cp "proxy:$src/$DOMAIN.key" "$tmp/privkey.pem"

mkdir -p "$BROKER_CERT_DIR"
if cmp -s "$tmp/fullchain.pem" "$BROKER_CERT_DIR/fullchain.pem"; then
  exit 0 # unchanged; nothing to do
fi

install -m 644 "$tmp/fullchain.pem" "$BROKER_CERT_DIR/fullchain.pem"
# Readable only by the container's mosquitto user (uid 1883).
install -m 600 -o 1883 -g 1883 "$tmp/privkey.pem" "$BROKER_CERT_DIR/privkey.pem"

# A restart disconnects every device; reconnect jitter (contract A2) spreads
# them out. Renewal happens about every 60 days.
compose restart mosquitto
echo "Broker certificate updated for $DOMAIN."
