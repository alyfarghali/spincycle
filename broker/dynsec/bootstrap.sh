<<<<<<< HEAD
#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/lib.sh"

for var in MQTT_INGEST_PASSWORD MQTT_API_PASSWORD MQTT_LOADGEN_PASSWORD; do
  if [[ "${!var:-change-me}" == "change-me" ]]; then
    echo "$var in .env is unset or still change-me." >&2
    exit 1
  fi
done

try() { ctl "$@" || echo "  skipped: dynsec $*" >&2; }

if compose run --rm --no-deps --user 1883:1883 --entrypoint sh mosquitto \
     -c 'test -f /mosquitto/data/dynamic-security.json'; then
  echo "Accounts file already exists; keeping it."
else
  echo "Creating accounts file with the admin account..."
  compose run --rm --no-deps --user 1883:1883 --entrypoint mosquitto_ctrl mosquitto \
    dynsec init /mosquitto/data/dynamic-security.json admin "$MQTT_ADMIN_PASSWORD"
fi

compose up -d mosquitto
wait_for_broker

echo "Creating ingest account..."
try createRole ingest
for kind in telemetry status; do
  try addRoleACL ingest subscribeLiteral "fleets/+/devices/+/$kind" allow
  try addRoleACL ingest subscribeLiteral "\$share/ingest/fleets/+/devices/+/$kind" allow
done
try createClient ingest -p "$MQTT_INGEST_PASSWORD"
try addClientRole ingest ingest

echo "Creating api account..."
try createRole api
try addRoleACL api publishClientSend 'fleets/+/devices/+/config' allow
try addRoleACL api publishClientSend '$CONTROL/dynamic-security/#' allow
try addRoleACL api subscribeLiteral '$CONTROL/dynamic-security/v1/response' allow
try createClient api -p "$MQTT_API_PASSWORD"
try addClientRole api api

echo "Creating loadgen account..."
try createRole loadgen
for kind in telemetry status; do
  try addRoleACL loadgen publishClientSend "fleets/loadtest/devices/+/$kind" allow
done
try addRoleACL loadgen subscribePattern 'fleets/loadtest/devices/+/config' allow
try createClient loadgen -p "$MQTT_LOADGEN_PASSWORD"
try addClientRole loadgen loadgen

echo "Done. Add a device with: broker/dynsec/device.sh add <fleet_id> <device_id>"
=======
# One-time setup of the dynamic security plugin: admin client, service accounts (ingest, api, loadgen) and roles.
>>>>>>> main
