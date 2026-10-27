# storage

Postgres 16 + TimescaleDB schema, as numbered migrations in `migrations/`.

| Migration | Creates |
|---|---|
| `0001_core_tables.sql` | `fleets`, `device_types`, `devices` (with `deleted_at` soft delete) |
| `0002_telemetry_hypertable.sql` | `telemetry` hypertable, index `(device_id, time DESC)` |
| `0003`–`0006` | *stubs*: hourly aggregate, retention/compression, subscriptions, busy times |
| `0007_readings_seen.sql` | `readings_seen` — dedup set on `(device_id, boot, seq)` |
| `0008_seed_device_types.sql` | `laundry-washer`, `laundry-dryer`, `loadtest-sim` |
| `0009_machine_events.sql` | `machine_events` — one row per state transition; index `(device_id, at DESC, id DESC)` |
| `0010_engine_params.sql` | Placeholder state-engine thresholds in `device_types.config` |

Never edit an applied migration: add a new one.

## Applying

A fresh database runs every migration automatically (Compose mounts this folder into Postgres's init
directory). An **existing** database doesn't, so apply new ones by hand:

```sh
docker compose --env-file .env -f deploy/docker-compose.yml exec -T postgres \
  psql -U spincycle -d spincycle -v ON_ERROR_STOP=1 < storage/migrations/0010_engine_params.sql
```

## Test database

`storage/test-db.sh` drops and recreates `spincycle_test` from every migration and prints the
`TEST_DATABASE_URL` to export. Go tests that need a database skip when it isn't set.

```sh
eval "$(storage/test-db.sh | tail -1)"
go test ./services/ingest/...
```

Port 5432 must belong to the Docker Postgres. A native Postgres on the same port shadows it
(`brew services stop postgresql@15`).

`dev_data.sql` is development-only seed data, loaded by hand.
