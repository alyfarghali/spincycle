#!/usr/bin/env bash
# One-time setup of the dynamic security plugin: admin client, service accounts (ingest, api, loadgen) and roles.
#
# Usage (from anywhere): broker/dynsec/bootstrap.sh
# Needs .env with the MQTT_*_PASSWORD values. Safe to re-run: existing accounts are reported and skipped.
set -euo pipefail
source "$(dirname "$0")/lib.sh"

for var in MQTT_INGEST_PASSWORD MQTT_API_PASSWORD MQTT_LOADGEN_PASSWORD; do
  if [[ "${!var:-change-me}" == "change-me" ]]; then
    echo "$var in .env is unset or still change-me." >&2
    exit 1
  fi
done

# Runs a dynsec command; on failure (usually "already exists" on a re-run) prints and continues.
try() { ctl "$@" || echo "  skipped: dynsec $*" >&2; }

# 1. Create the accounts file with the admin account. The broker can't start
#    with the plugin enabled until this file exists, so it runs in a one-off
#    container as the mosquitto user (uid 1883), which owns the data volume.
if compose run --rm --no-deps --user 1883:1883 --entrypoint sh mosquitto \
     -c 'test -f /mosquitto/data/dynamic-security.json'; then
  echo "Accounts file already exists; keeping it."
else
  echo "Creating accounts file with the admin account..."
  compose run --rm --no-deps --user 1883:1883 --entrypoint mosquitto_ctrl mosquitto \
    dynsec init /mosquitto/data/dynamic-security.json admin "$MQTT_ADMIN_PASSWORD"
fi

# 2. Start the broker and wait until it takes admin commands.
compose up -d mosquitto
wait_for_broker

# 3. ingest: receives every device's telemetry and status, nothing else.
#    Mosquitto checks the subscription either with or without the $share/ingest/
#    prefix; both forms are allowed until an experiment shows which one is used.
echo "Creating ingest account..."
try createRole ingest
for kind in telemetry status; do
  try addRoleACL ingest subscribeLiteral "fleets/+/devices/+/$kind" allow
  try addRoleACL ingest subscribeLiteral "\$share/ingest/fleets/+/devices/+/$kind" allow
done
try createClient ingest -p "$MQTT_INGEST_PASSWORD"
try addClientRole ingest ingest

# 4. api: publishes retained device config and manages accounts through the
#    dynsec control topic. That makes this credential as powerful as admin.
echo "Creating api account..."
try createRole api
try addRoleACL api publishClientSend 'fleets/+/devices/+/config' allow
try addRoleACL api publishClientSend '$CONTROL/dynamic-security/#' allow
try addRoleACL api subscribeLiteral '$CONTROL/dynamic-security/v1/response' allow
try createClient api -p "$MQTT_API_PASSWORD"
try addClientRole api api

# 5. loadgen: acts as any device, but only inside the loadtest fleet.
echo "Creating loadgen account..."
try createRole loadgen
for kind in telemetry status; do
  try addRoleACL loadgen publishClientSend "fleets/loadtest/devices/+/$kind" allow
done
try addRoleACL loadgen subscribePattern 'fleets/loadtest/devices/+/config' allow
try createClient loadgen -p "$MQTT_LOADGEN_PASSWORD"
try addClientRole loadgen loadgen

echo "Done. Add a device with: broker/dynsec/device.sh add <fleet_id> <device_id>"
