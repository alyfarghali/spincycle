# Roles

Role definitions: per-device topic ACLs bound to the device's own fleet, plus service account permissions.

Created by [bootstrap.sh](bootstrap.sh) (service accounts) and [device.sh](device.sh) (devices). Anything not listed is denied, except receiving messages a client is subscribed to, which dynsec allows by default.

| Account | Role | May publish to | May subscribe to |
|---|---|---|---|
| `admin` | `admin` (created by `dynsec init`) | `$CONTROL/dynamic-security/#` | its responses |
| `ingest` | `ingest` | nothing | `fleets/+/devices/+/telemetry` and `…/status`, with or without `$share/ingest/` |
| `api` | `api` | `fleets/+/devices/+/config`, `$CONTROL/dynamic-security/#` | `$CONTROL/dynamic-security/v1/response` |
| `loadgen` | `loadgen` | `fleets/loadtest/devices/+/telemetry` and `…/status` | `fleets/loadtest/devices/+/config` |
| `{device_id}` | `device-{device_id}` | exactly `fleets/{fleet}/devices/{device_id}/telemetry` and `…/status` | exactly `fleets/{fleet}/devices/{device_id}/config` |

Notes:

- **Devices are pinned to one fleet.** The device role names the full topic, so a leaked password can't publish as another device or into another fleet.
- **The api credential is as powerful as admin**, because it can send any dynsec command. Protect it the same way.
- **Client IDs:** each device's client ID is pinned to its device ID (`createClient -c`). Service accounts aren't pinned, so several ingest replicas (`ingest-1`, `ingest-2`, …) can share the `ingest` account.
- **Ingest client IDs must be stable.** Measured Oct 14, 2026: Mosquitto 2.0.20 deals `$share/ingest` messages round-robin to every group member with a persistent session, **including offline ones**. With one live and one offline member, 100 of 200 readings were parked in the offline session until it reconnected (or would be dropped when it expired after `SessionExpiryInterval`, 1 h). So reuse a fixed set of IDs (`ingest-1` on the host and in Compose; run one at a time), and when scaling down, clear a retired ID's session by connecting once with it, clean start and session expiry 0.
- **Open question:** whether Mosquitto checks a shared subscription with or without the `$share/ingest/` prefix. Both are allowed for now; remove the unused one once tested.
