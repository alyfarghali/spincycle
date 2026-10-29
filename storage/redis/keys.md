# keys

Redis is a **cache** (persistence off). Postgres is the record of truth; everything here is rebuilt from
it and from new readings. Written by ingest (`services/ingest/livestate`).

| Key | Type | Contents | Updated |
|---|---|---|---|
| `device:{id}:latest` | hash | `ts`, `boot`, `seq`, one field per metric (e.g. `vibration_rms`) | every stored reading |
| `device:{id}:last_seen` | string | Unix seconds the reading was received | every stored reading |
| `device:{id}:state` | hash | `state`, `since` (Unix ms) | every stored reading and transition |
| `fleet:{fleet}:state` | hash | device ID → `state\|since_ms` for the whole fleet: one `HGETALL` reads every device | same |
| `fleet:{fleet}:counts` | hash | state → number of devices in it | same, atomically with `fleet:{fleet}:state` |
| `fleet:{fleet}:readings:{unix_s}` | string | readings received in that second; expires after 2 min | every stored reading |
| `fleet:{fleet}:transitions:{unix_s}` | string | transitions in that second; expires after 2 min | every transition |
| `events` | pub/sub | `{"device_id","fleet_id","from","to","at"}` per transition | every transition |

`fleet:{fleet}:state` and `fleet:{fleet}:counts` change together in one Lua script, which reads the
device's previous state and moves its count, so the counts always equal a tally of the map whatever
order updates arrive in.

## If Redis is wiped or restarted

- On startup, and on the next 30 s tick after any missed write, ingest rebuilds every fleet's map and
  counts from the machine states it holds (restored from `machine_events`).
- `device:{id}:latest` and `last_seen` refill with each device's next reading.
- Rates restart from zero; past seconds are gone, which only affects the last 2 minutes of the chart.
