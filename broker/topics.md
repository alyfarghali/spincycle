<<<<<<< HEAD
# Topics

Topic layout (telemetry, status, config): direction, QoS, retained flag, payload. Mirrors [contract A3](../docs/telemetry-contract.md); if they disagree, the contract wins.

| Topic | Who publishes | Who subscribes | QoS | Retained | Payload |
|---|---|---|---|---|---|
| `fleets/{fleet}/devices/{id}/telemetry` | the device | ingest, as `$share/ingest/…` | 1 | no | JSON reading (contract A4) |
| `fleets/{fleet}/devices/{id}/status` | the device (`online`); the broker via Last Will (`offline`) | ingest, as `$share/ingest/…` | 1 | yes | `online` / `offline` |
| `fleets/{fleet}/devices/{id}/config` | the API | the device | 1 | yes | JSON config (contract A7) |
| `$CONTROL/dynamic-security/v1` | admin, API | the broker's dynsec plugin | 1 | no | dynsec commands |
| `$CONTROL/dynamic-security/v1/response` | the broker | admin, API | — | no | command results |
| `$SYS/#` | the broker | monitoring, during load tests | — | — | broker statistics |
=======
# topics

Topic layout (telemetry, status, config): direction, QoS, retained flag, payload.
>>>>>>> main
