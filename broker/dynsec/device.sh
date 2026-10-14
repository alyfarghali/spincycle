#!/usr/bin/env bash
# Issues or revokes one device's broker credential. Manual stand-in until POST/DELETE /api/admin/devices exist.
#
# Usage:
#   broker/dynsec/device.sh add <fleet_id> <device_id>   prints the device's password once
#   broker/dynsec/device.sh remove <device_id>           revokes it; the device is disconnected
set -euo pipefail
source "$(dirname "$0")/lib.sh"

id_re='^[a-z0-9][a-z0-9-]{0,31}$' # contract A1

usage() { sed -n '4,6p' "$0" >&2; exit 1; }

add() {
  local fleet="$1" device="$2"
  [[ $fleet =~ $id_re && $device =~ $id_re ]] || { echo "IDs must match $id_re" >&2; exit 1; }

  local base="fleets/$fleet/devices/$device"
  local role="device-$device"
  local password
  password="$(openssl rand -hex 32)" # contract A2: 32 random bytes as 64 hex chars

  wait_for_broker
  # The role names this device's exact topics, fleet included, so its password
  # can't publish as another device or into another fleet.
  ctl createRole "$role"
  ctl addRoleACL "$role" publishClientSend "$base/telemetry" allow
  ctl addRoleACL "$role" publishClientSend "$base/status" allow
  ctl addRoleACL "$role" subscribeLiteral "$base/config" allow
  # -c pins the client ID to the device ID (contract A2).
  ctl createClient "$device" -c "$device" -p "$password"
  ctl addClientRole "$device" "$role"

  cat <<EOF

Device $device registered in fleet $fleet.
MQTT password (shown once, not stored anywhere else):

  $password

Write it into the device's NVS with firmware/tools/provision.py.
EOF
}

remove() {
  local device="$1"
  wait_for_broker
  # Deleting the client also disconnects it if it's online.
  ctl deleteClient "$device"
  ctl deleteRole "device-$device"
  echo "Device $device revoked."
}

case "${1:-}" in
  add) [[ $# -eq 3 ]] || usage; add "$2" "$3" ;;
  remove) [[ $# -eq 2 ]] || usage; remove "$2" ;;
  *) usage ;;
esac
