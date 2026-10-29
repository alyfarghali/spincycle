# loadgen

Simulates up to 10,000 sensor devices publishing live telemetry through the real broker, ingest and
database, to measure the platform at design load (CLAUDE.md §3: 2,000 msg/s).

## Run

```sh
docker compose --env-file .env -f deploy/docker-compose.yml exec -T postgres \
  psql -U spincycle -d spincycle -v ON_ERROR_STOP=1 < tools/loadgen/seed.sql   # once per database
set -a; source .env; set +a
go run ./tools/loadgen -devices 10000
```

| Flag | Default | Meaning |
|---|---|---|
| `-devices` | 1 | Number of virtual devices |
| `-interval` | 5s | Reading period per device |
| `-ramp` | 60s | Spread device start-up over this long, so the test measures steady state, not a TLS handshake burst |
| `-duration` | 0 (until Ctrl+C) | Run time after the ramp |
| `-shared` | false | All devices on one connection: tests message rate only, not connection count |
| `-broker` | `mqtts://localhost:8883` | Broker URL |
| `-ca` | `broker/certs/dev/ca.pem` | CA for the broker certificate; empty uses system CAs. Verification is never disabled |
| `-fleet` | `loadtest` | The `loadgen` broker account may only publish to `fleets/loadtest/...` |

Progress every 5 s: connected devices, published total and rate, failures.

## How a virtual device behaves

Each device mirrors a real board (contract A2–A6):
- **Own connection** (default), client ID = device ID, keepalive 30 s, reconnect backoff 1–60 s.
- **Last Will** `offline` (retained) on its status topic; `online` (retained) on connect; `offline` before a clean disconnect.
- **Telemetry** every `-interval` at QoS 1, contract A4 payload with `fw: "loadgen-0.1.0"`.
- **`boot` = run start time** (Unix seconds), so a rerun isn't discarded by ingest's `(device, boot, seq)` dedup.
- **Signal**: a 60 min cycle (35 min wash, then idle) from a random phase, at the bench-measured levels
  (idle 0.0015 g, wash 0.027 g, ±10 % noise). To be replaced by replaying `testdata/cycles` recordings.

Device IDs `loadtest-00001` … `loadtest-10000` match `seed.sql`, which registers them so ingest accepts them.

## Files

| File | Contents |
|---|---|
| `main.go` | Flags, connections (`connect`), publishing loop (`run`), clean shutdown (`goOffline`), `deviceID`, `topic`, `tlsConfig` |
| `device.go` | `device` and `reading`: one contract payload per call |
| `seed.sql` | Registers the `loadtest` fleet and 10,000 devices |
| `pool.go`, `ramp.go`, `report.go` | Stubs |

## Tests

```sh
go test ./tools/loadgen -v
```

| Test | Spec it checks |
|---|---|
| `TestReadingPassesIngestValidation` | Payloads pass **ingest's own decoder** (contract A9) with the right device, boot, seq, ms timestamp and metric |
| `TestReadingEnvelope` | Exactly the A4 fields, `v` 1, `fw` starts `loadgen-`, `ts` UTC with ms and `Z`, finite metric, ≤ 1024 bytes |
| `TestSeqCountsUpAndBootStays` | `seq` 0, 1, 2 … per device; `boot` fixed for the run |
| `TestWashCycleLevels` | 35 of every 60 minutes at wash level, the rest idle, within the noise band |
| `TestDeviceIDsMatchContractAndSeed` | All 10,000 IDs match the A1 pattern, are unique, and still match `seed.sql` |
| `TestTopics` | Telemetry and status topics follow A3 |
| `TestTLSConfigAlwaysVerifies` | System or custom CA; bad CA files fail; verification never disabled (CLAUDE.md §10) |

Proven able to fail: dropping the `loadgen-` prefix, dropping milliseconds from `ts`, a `seq` that never
increments, IDs that no longer match `seed.sql`, and `InsecureSkipVerify` each turn at least one test red.

## Results

See `docs/load-tests.md`. Baseline (naive ingest, one INSERT per message, everything on one laptop):
10,000 connections, ~2,000 msg/s published, every reading stored, but storage lagged behind under load
and caught up at ~3,050 rows/s after the run.
