# ingest

Go service that receives telemetry from the broker, validates and deduplicates it, stores it, and
(in progress) runs the state engine.

## Pipeline

One worker loop, in order, for each message:
1. **Subscribe** (`subscriber/`): `$share/ingest/fleets/+/devices/+/telemetry`, MQTT 5 persistent session,
   manual acks.
2. **Decode** (`decoder/`): contract A9 validation, rejects counted by reason.
3. **Save** (`dedup/`): `readings_seen` + `telemetry` in one transaction. Already seen → duplicate.
4. **Ack** only after the commit, so a crash means redelivery, never loss.

## Registry (`registry/`)

Loads every non-deleted device with its fleet, type, declared metrics and **state-engine parameters**
(`device_types.config` merged with `devices.config`, see `engine/README.md`). A device whose config doesn't
parse keeps `EngineErr` set: its readings are still stored, it just gets no state.

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
| `decoder` | `decoder_test.go` | Contract A9 rejections |
| `engine` | see `engine/README.md` | |
