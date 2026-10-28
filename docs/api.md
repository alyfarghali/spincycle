# API

HTTP API served by `services/api` behind Caddy at `/api`. It's the contract between the API and the web
page: change both together. Version 1.

Designed for fleets of 10,000 devices viewed live by many people at once:
- **Live state comes from Redis**, kept current by ingest (`storage/redis/keys.md`). Per-fleet data is
  one Redis read, never one per device.
- **Fleet responses are cached in the API for 2 s** and sent with `Cache-Control: max-age=2`, so the
  number of viewers doesn't change the load on Redis.
- **The whole fleet's live state is one short string**, one character per device.
- Responses are gzip-compressed when the client accepts it.

## Conventions

- JSON, UTF-8. Timestamps are RFC 3339 UTC with milliseconds (`2026-10-27T14:03:11.250Z`), except in
  `/states`, which has none.
- **States:** `free`, `running`, `finished`, `offline`, `unknown` (back after a long dropout, state not
  resolved yet), and `none` (registered but no state yet: never reported, or its config doesn't parse).
- **Online** means the state is `free`, `running`, `finished` or `unknown`.
- **`kind`** comes from the device type: `laundry-washer` → `washer`, `laundry-dryer` → `dryer`; any other
  type → the type ID (e.g. `loadtest-sim`).
- Devices are always ordered by ID. Soft-deleted devices never appear.

### Errors

Every error has the same shape and an HTTP status that matches:

```json
{ "error": "fleet_not_found", "message": "No fleet with ID 'laundy'." }
```

| `error` | Status | When |
|---|---|---|
| `fleet_not_found` | 404 | Unknown fleet ID |
| `machine_not_found` | 404 | Unknown or deleted device ID |
| `invalid_query` | 400 | Bad `state`, `limit` or `after` |
| `unavailable` | 503 | Postgres or Redis unreachable; the page shows its "can't reach the server" banner |

---

## `GET /health`

200 if Postgres and Redis both respond, otherwise 503. For uptime checks; not cached.

```json
{ "status": "ok", "postgres": "ok", "redis": "ok" }
```
```json
{ "status": "down", "postgres": "ok", "redis": "down" }
```

## `GET /api/fleets/{fleet}/summary`

Counts and throughput for the whole fleet. The header and charts of the page. Cached 2 s.

```json
{
  "fleet": "loadtest",
  "name": "Load test",
  "devices": 10000,
  "online": 9950,
  "counts": { "free": 4100, "running": 5800, "finished": 30, "offline": 50, "unknown": 20, "none": 0 },
  "readings_per_s": [1998, 2003, 2001],
  "transitions_per_s": [3, 0, 5],
  "generated_at": "2026-10-27T14:03:11.250Z"
}
```

| Field | Meaning |
|---|---|
| `devices` | Registered, non-deleted devices in the fleet |
| `counts` | Devices per state; always sums to `devices` (`none` is the remainder) |
| `readings_per_s`, `transitions_per_s` | The last 60 **complete** seconds, oldest first; 0 for a second with nothing |

## `GET /api/fleets/{fleet}/devices`

The fleet's device list, in the order `/states` uses. Changes rarely: fetch on load, and again only when
`/states` reports a different `version`. Cached 60 s.

```json
{
  "fleet": "loadtest",
  "version": "9f2c1a7e",
  "devices": [
    { "id": "loadtest-00001", "name": "Sim 1", "kind": "loadtest-sim" },
    { "id": "loadtest-00002", "name": "Sim 2", "kind": "loadtest-sim" }
  ]
}
```

`version` is a hash of the ordered device IDs: it changes when a device is added or removed.

## `GET /api/fleets/{fleet}/states`

Every device's live state as one string, aligned with `/devices`. The fleet map polls this every 5 s.
Cached 2 s.

```json
{ "fleet": "loadtest", "version": "9f2c1a7e", "states": "frrrd-ofru" }
```

| Character | State |
|---|---|
| `f` | free |
| `r` | running |
| `d` | finished (done) |
| `o` | offline |
| `u` | unknown |
| `-` | none |

`states[i]` is the state of `devices[i]` from `/devices` with the same `version`. If the versions differ,
refetch `/devices` before drawing.

## `GET /api/fleets/{fleet}/machines`

Full detail, a page at a time: the text list behind a clicked count, and the card grid for small fleets.
Cached 2 s.

| Query | Default | Meaning |
|---|---|---|
| `state` | all | Only machines in this state (any state above, including `none`) |
| `limit` | 100 | 1–500 |
| `after` | — | Return machines with IDs after this one: pass the previous page's `next` |

```json
{
  "fleet": "laundry",
  "machines": [
    {
      "id": "c6-washer-03",
      "name": "Washer 3",
      "kind": "washer",
      "state": "running",
      "since": "2026-10-27T13:29:04.000Z",
      "online": true,
      "last_seen": "2026-10-27T14:03:10.000Z",
      "latest": { "vibration_rms": 0.0423, "ts": "2026-10-27T14:03:09.000Z" }
    }
  ],
  "next": null
}
```

`since`, `last_seen` and `latest` are `null` for a machine that has none yet. `next` is `null` on the
last page.

## `GET /api/machines/{id}`

One machine, with its 10 most recent transitions (newest first): what the hover/tap detail and the
`/m/{id}` page show. Cached 2 s.

```json
{
  "id": "c6-washer-03",
  "fleet": "laundry",
  "name": "Washer 3",
  "kind": "washer",
  "state": "running",
  "since": "2026-10-27T13:29:04.000Z",
  "online": true,
  "last_seen": "2026-10-27T14:03:10.000Z",
  "latest": { "vibration_rms": 0.0423, "ts": "2026-10-27T14:03:09.000Z" },
  "events": [
    { "from": "free", "to": "running", "at": "2026-10-27T13:29:04.000Z" }
  ]
}
```

---

## Sizes at 10,000 devices

| Response | Raw | Compressed (estimate) | How often per viewer |
|---|---|---|---|
| `/summary` | ~1.5 KB (two 60-number arrays) | < 1 KB | every 5 s |
| `/states` | ~10 KB (1 byte per device) | a few KB: ~6 states ≈ 2–3 bits per device | every 5 s |
| `/devices` | ~550 KB (~55 bytes per device) | ~60–80 KB | once, then on `version` change |

For comparison, one full JSON object per device every 5 s would be ~1.1 MB per viewer per refresh.

## Targets (measured in Stage 3, Feature 4)

- Freshness: a transition visible on the page within 10 s (CLAUDE.md §3): ≤ 2 s cache + ≤ 5 s poll.
- `/summary` and `/states` at 10,000 devices: p99 latency and requests per second under many concurrent
  viewers, recorded in `docs/load-tests.md`.
