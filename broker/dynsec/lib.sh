root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

if [[ ! -f "$root/.env" ]]; then
  echo "Missing $root/.env. Copy .env.example to .env and fill it in." >&2
  exit 1
fi
set -a
source "$root/.env"
set +a

if [[ "${MQTT_ADMIN_PASSWORD:-change-me}" == "change-me" ]]; then
  echo "MQTT_ADMIN_PASSWORD in .env is unset or still change-me." >&2
  exit 1
fi

cert_dir="${BROKER_CERT_DIR:-$root/broker/certs/dev}"
[[ $cert_dir == /* ]] || cert_dir="$root/deploy/$cert_dir"
if [[ ! -f "$cert_dir/fullchain.pem" || ! -f "$cert_dir/privkey.pem" ]]; then
  echo "No broker certificate in $cert_dir. For development run broker/certs/make-dev-certs.sh." >&2
  exit 1
fi

compose() {
  docker compose --project-directory "$root/deploy" --env-file "$root/.env" \
    -f "$root/deploy/docker-compose.yml" "$@"
}

ctl() {
  compose exec -T mosquitto mosquitto_ctrl -h localhost -p 1883 \
    -u admin -P "$MQTT_ADMIN_PASSWORD" dynsec "$@"
}

wait_for_broker() {
  for _ in $(seq 1 30); do
    if ctl getDefaultACLAccess >/dev/null 2>&1; then return 0; fi
    sleep 1
  done
  echo "Broker did not become ready in 30 s. Check: docker compose logs mosquitto" >&2
  exit 1
}
