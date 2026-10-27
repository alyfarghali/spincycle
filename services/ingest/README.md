# ingest

Go service that receives telemetry from the broker, validates and deduplicates it, stores it, and
(in progress) runs the state engine.

## Pipeline

One worker loop, in order, for each message:
1. **Subscribe** (`subscriber/`): `$share/ingest/fleets/+/devices/+/telemetry` and `…/status`, MQTT 5
   persistent session, manual acks.
2. **Decode** (`decoder/`): contract A9 validation, rejects counted by reason.
3. **Save** (`dedup/`): `readings_seen` + `telemetry` + the reading's state transitions in one transaction.
   Already seen → duplicate, and nothing is written.
4. **Ack** only after the commit, so a crash means redelivery, never loss.

## Status messages (`decoder/status.go`)

`DecodeStatus(topic, payload, devices)` handles contract A6: plain-text `online` / `offline` on
`fleets/{fleet}/devices/{id}/status`, from the device or the broker's Last Will. Same topic checks as
telemetry (registered device, matching fleet). Anything else, including a trailing newline, is
`bad_status`. `offline` drives the engine's `OnOffline`; `online` changes nothing, since the next
reading already counts as back online.

## Registry (`registry/`)

Loads every non-deleted device with its fleet, type, declared metrics and **state-engine parameters**
(`device_types.config` merged with `devices.config`, see `engine/README.md`). A device whose config doesn't
parse keeps `EngineErr` set: its readings are still stored, it just gets no state.

## Storage (`dedup/store.go`)

| Method | Does |
|---|---|
| `Save(ctx, reading, transitions)` | Dedup mark, telemetry rows and `machine_events` rows in **one transaction**. Returns `false` for a duplicate (nothing written). If any insert fails, the whole reading rolls back and isn't acked, so redelivery retries all of it |
| `SaveTransitions(ctx, device, transitions)` | Transitions not caused by a reading (Last Will, sweeper tick), in their own transaction |
| `LoadStates(ctx, now)` | Restores each machine from its newest `machine_events` row. An offline machine also gets `Prev` (from `from_state`), so it can still resume. `LastReading` is set to `now`, so after a restart every device gets a full `offline_after` before being marked offline. Not restored: the shaking/quiet run timers (a cycle in progress re-confirms) and the true start of the state before offline |

Why one transaction: if the transition were written separately and ingest crashed in between, the
redelivered reading would be dropped as a duplicate and the transition would be lost for good.

## State engine (`engine/`)

Pure transition logic, see `engine/README.md`. Wiring into this pipeline is in progress.

## Tests

```sh
eval "$(storage/test-db.sh | tail -1)"   # from the repo root; see storage/README.md
go test ./services/ingest/...
```

| Package | Test | Checks |
|---|---|---|
| `registry` | `TestLoadParsesEngineParams` | Type config loaded, device override applied, broken config reported but device kept, soft-deleted devices excluded (needs `TEST_DATABASE_URL`) |
| `dedup` | `store_test.go` | Reading + transitions written together; a duplicate writes nothing; a failing transition rolls the reading back; standalone transitions; restore picks the newest transition and keeps `Prev` for offline (needs `TEST_DATABASE_URL`) |
| `decoder` | `decoder_test.go` | Contract A9 rejections |
| `decoder` | `status_test.go` | Contract A6: both payloads; newline, case, JSON and empty rejected; unknown device, wrong fleet, wrong or malformed topic |
| `engine` | see `engine/README.md` | |
