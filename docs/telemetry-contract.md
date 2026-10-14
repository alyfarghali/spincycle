# Telemetry contract v1

**Status:** Frozen, Oct 12, 2026.
**Approved:** Aly Farghali (2026-10-12) · Twyla (pending)

This document is the interface between devices and the platform. Firmware, the ingest service, the load generator, the demo replayer and test fixtures all implement it. If they disagree with it, they are wrong.

It has three parts:

- **Part A, platform contract.** Applies to every device of every type: identity, connection, topics, the telemetry envelope, delivery guarantees, validation. Changing it is a contract change.
- **Part B, metric definitions.** The platform-wide meaning of each metric name. Adding a metric is additive and doesn't change the contract version.
- **Part C, laundry application.** What the laundry application adds on top: recorded cycle files and how they're turned into tests.

A new IoT use case is a new row in `device_types` plus, if needed, new metric definitions in Part B. It never requires a change to Part A.

Words in **must** / **must not** / **should** are requirements. Everything else is explanation.

---

# Part A: Platform contract

## A1. Identifiers

| Identifier | Format | Examples |
|---|---|---|
| `device_id` | `^[a-z0-9][a-z0-9-]{0,31}$` | `c6-washer-03`, `c6-dryer-01` |
| `fleet_id` | `^[a-z0-9][a-z0-9-]{0,31}$` | `laundry`, `demo`, `loadtest` |
| device type ID | `^[a-z0-9][a-z0-9-]{0,31}$` | `laundry-washer`, `laundry-dryer`, `loadtest-sim` |

- Lowercase letters, digits and `-`, 1–32 characters, starting with a letter or digit.
- The device ID is also the MQTT username and a topic level, so `/`, `+`, `#` and spaces are impossible by construction.
- `device_id` is unique across the whole platform, not only within a fleet.
- Fleets `demo` and `loadtest` are reserved for demo replay and load testing.
- A device's fleet and type are set at registration and live in the registry. They are never sent by the device.

## A2. Connection

| Setting | Value |
|---|---|
| Broker URL | `mqtts://<host>:8883` |
| Fallback URL | `wss://<host>:443/mqtt` (for networks that block 8883) |
| MQTT version (device) | 3.1.1 |
| Client ID | `device_id` |
| Username | `device_id` |
| Password | 64 lowercase hex characters (32 random bytes), issued once by `POST /api/admin/devices` |
| Keepalive | 30 s |
| Clean session | `true` (config is retained, so the device misses nothing) |
| TLS | Server certificate **must** be validated, including hostname. Disabling verification is never allowed, including in testing. |
| Last Will | topic `…/status`, payload `offline`, QoS 1, retained |

**On every successful connect, in this order:**
1. Publish `online` to `…/status` (QoS 1, retained).
2. Subscribe to `…/config` (QoS 1).
3. Flush the offline queue (A5), then resume normal publishing.

**Reconnect:** exponential backoff 1, 2, 4, 8 … capped at 60 s, with ±20 % random jitter. Without jitter, thousands of devices reconnect in lockstep after a broker restart and the TLS handshakes arrive as one spike.

With a 30 s keepalive the broker publishes the Last Will about 45 s after a device goes silent.

## A3. Topics

| Topic | Direction | QoS | Retained | Payload |
|---|---|---|---|---|
| `fleets/{fleet_id}/devices/{device_id}/telemetry` | device → platform | 1 | no | JSON, A4 |
| `fleets/{fleet_id}/devices/{device_id}/status` | device or broker (LWT) → platform | 1 | yes | `online` / `offline`, A6 |
| `fleets/{fleet_id}/devices/{device_id}/config` | platform → device | 1 | yes | JSON, A7 |

- A device **must** only publish to its own `telemetry` and `status` topics and only subscribe to its own `config` topic. The broker enforces this per device, including the fleet.
- **The topic is authoritative.** The platform identifies the device from the topic, never from the payload.

## A4. Telemetry envelope

```json
{
  "v": 1,
  "device_id": "c6-washer-03",
  "fw": "0.1.0",
  "boot": 12,
  "seq": 1842,
  "ts": "2026-10-20T14:03:11.250Z",
  "metrics": { "vibration_rms": 0.0423 }
}
```

| Field | Type | Required | Rules |
|---|---|---|---|
| `v` | integer | yes | Contract version. `1` for this document. |
| `device_id` | string | yes | **Must** equal the `{device_id}` in the topic. |
| `fw` | string | yes | Firmware version, semver for real devices. Simulators send `loadgen-<version>`, demo replay sends `demo-<version>`. Used for tracing which code produced a reading. |
| `boot` | integer, uint32 | yes | Starts at 1. Incremented **and committed to non-volatile storage before the first publish** of each power-up, so a crash can never reuse a boot number. |
| `seq` | integer, uint64 | yes | Starts at 0 each boot. Incremented once per reading period, **including periods that are skipped or dropped**, so gaps are visible. |
| `ts` | string or `null` | yes | Time the reading describes (for windowed metrics: the last sample in the window). RFC 3339, UTC, `Z` suffix, millisecond precision. `null` until the device clock has synced. |
| `metrics` | object | yes | Map of metric name (Part B) → finite number. At least one entry. |

- **Encoding:** UTF-8 JSON object, at most 1024 bytes. Field order doesn't matter.
- **Identity:** `(device_id, boot, seq)` uniquely identifies a reading. `seq` will never approach 2⁵³, so it is safe as a JSON number.
- **Forward compatibility:** the platform ignores unknown top-level fields. It rejects an unknown `v`.

## A5. Timing and the offline queue

- `ts` comes from network time (SNTP). Before the first sync, `ts` is `null` and the platform substitutes its receive time.
- The platform accepts `ts` within ±1 h of its own clock.
- While disconnected, a device **should** keep at least 5 minutes of readings in RAM, dropping the oldest when full.
- **Only readings with a non-null `ts` are queued.** A reading with `ts: null` made while disconnected is dropped, because the platform would stamp it with the wrong time.
- On reconnect the queue is flushed oldest first, before any new readings.

## A6. Status payload

Plain UTF-8 text, no JSON, no newline:

| Payload | Published by | When |
|---|---|---|
| `online` | device | after every successful connect |
| `offline` | broker (Last Will) | unclean disconnect or missed keepalive |
| `offline` | device | before a deliberate disconnect |

The platform treats `offline` as an immediate offline signal. The next valid telemetry reading counts as back online, whether or not `online` arrived first.

## A7. Config payload

```json
{ "v": 1, "interval_s": 5 }
```

| Field | Type | Rules |
|---|---|---|
| `v` | integer | `1` |
| `interval_s` | integer | 5–60. The reading period. For windowed metrics it is also the window length. |
| other keys | any | Defined by the device type. Devices ignore keys they don't know. |

- Published retained by the platform whenever a device's config changes, so a reconnecting device always gets the current value.
- The device applies a new value at the next period boundary and stores it so it's used before the next connect.
- An invalid value (wrong type, out of range, unknown `v`) is ignored and logged; the device keeps its current config.

## A8. Delivery guarantees

These are what every consumer inside the platform (storage, live state, application processors) may rely on, and nothing more:

| Guarantee | Meaning |
|---|---|
| **At least once** | A reading the broker accepted is stored at least once, as long as the platform eventually recovers. It may be delivered more than once. |
| **Deduplicated storage** | Storage keeps each `(device_id, boot, seq)` at most once. |
| **No ordering** | Readings may be processed in any order, including across replicas and after an offline-queue flush. Consumers that need order use `(boot, seq)` and **must** tolerate late readings. |

## A9. Platform-side validation

What ingest does with every telemetry message, in order. Every outcome is counted by reason on `/metrics`.

| Check | On failure |
|---|---|
| Payload ≤ 1024 bytes | reject `payload_too_large` |
| Valid JSON object | reject `invalid_json` |
| `v` is a known version | reject `unknown_version` |
| Topic device is registered and not deleted | reject `unknown_device` |
| Topic fleet matches the device's registered fleet | reject `fleet_mismatch` |
| Payload `device_id` matches topic | reject `device_mismatch` |
| Field types match A4 | reject `bad_type` |
| `ts` is null or within ±1 h | reject `ts_out_of_range` |
| Each metric is declared by the device's type | drop that metric, count `undeclared_metric`; reject the message `no_metrics` if none remain |
| `(device_id, boot, seq)` not already accepted | drop as `duplicate` (counted, not an error) |

Duplicate detection is **set membership**, not "seq must be greater than the last". A comparison against the last seen value would discard valid readings that arrive out of order (A8).

---

# Part B: Metric definitions

A metric name has **one meaning across the whole platform**: one unit and one way of computing it. Device types declare which metrics they send (`device_types.metrics`), and the platform stores every metric the same way.

Rules:

- Names are `snake_case` and include the quantity, e.g. `vibration_rms`, `temperature_c`.
- **Adding** a metric is additive: add a section here and declare it on a device type. No `v` bump.
- **Changing** how a metric is computed (filtering, sample rate, range, window) creates a **new name** (e.g. `vibration_rms_hp`). Data with different meanings never shares a name, so stored history and thresholds stay valid.

## B1. `vibration_rms`

| Property | Value |
|---|---|
| Unit | g |
| Sample rate | 100 Hz on x, y, z |
| Accelerometer range | ±2 g |
| Window | `interval_s` seconds → N = 100 × `interval_s` samples |
| Used by | `laundry-washer`, `laundry-dryer`, `loadtest-sim` |

For each window:

1. For each sample, magnitude `mᵢ = √(xᵢ² + yᵢ² + zᵢ²)`.
2. Remove gravity and slow drift with the window mean: `dᵢ = mᵢ − mean(m)`.
3. `vibration_rms = √(mean(dᵢ²))`.

- No other filtering.
- At least 4 significant digits.
- If fewer than 90 % of the expected samples were collected, the window is **skipped**: nothing is published, but `seq` is still incremented.

---

# Part C: Laundry application

The laundry application is one consumer of the platform. It runs a state engine over `vibration_rms` for devices of type `laundry-washer` and `laundry-dryer`. Its thresholds live in `device_types.config` and `devices.config`, not in this contract.

## C1. Recorded cycle CSVs

Recordings in `testdata/cycles/` are the ground truth the state engine is tested against.

**File name:** `{kind}-{loader}-{yyyymmdd}-{n}.csv`, e.g. `washer-front-20261016-1.csv`, `dryer-na-20261016-1.csv`.

**Columns:**

```csv
ts,vibration_rms,event
2026-10-16T19:02:05.000Z,0.0031,
2026-10-16T19:02:10.000Z,0.0412,start
2026-10-16T19:02:15.000Z,0.0398,
...
2026-10-16T19:41:20.000Z,0.0029,end
```

| Column | Rules |
|---|---|
| `ts` | Same format as telemetry `ts`. Never null: record only after the clock has synced. |
| `vibration_rms` | Exactly as defined in B1. |
| `event` | Empty, `start` or `end`, filled in by the person watching: `start` when the cycle begins, `end` when it has visibly finished. |

**Metadata:** one row per recording in the table in `testdata/cycles/README.md`: file, machine model, mount, `fw` version, notes (e.g. "long soak pause at 12 min", "neighbor dryer running").

## C2. Expected transitions

| Event | The engine must report | Within |
|---|---|---|
| `start` | `running` | `start_confirm` + 60 s after the event |
| `end` | `finished` | `stop_confirm` + 60 s after the event |
| between `start` and `end` | never `finished` or `free` | — |

---

# Changing this contract

| Change | Process |
|---|---|
| New device type | Add a `device_types` row (migration or admin API). No contract change. |
| New metric | Add a Part B section in a PR. Additive; no `v` bump. |
| New application rules | Edit that application's part. Doesn't affect other applications. |
| **Anything in Part A** | Both owners approve. Bump `v`. The same PR (or a linked set merged together) updates firmware, the ingest decoder, the load generator, the demo replayer and fixtures. During a migration the platform accepts both versions. |

# Changelog

| Version | Date | Change |
|---|---|---|
| v1 | 2026-10-12 | First frozen version. Differences from the design doc: added `v` and `fw`; `ts` precision and window semantics; topic is authoritative over payload `device_id`; ID rules including device type IDs; keepalive, client ID, password encoding, reconnect jitter; null-`ts` readings aren't queued offline; config gains `v`, a 5–60 s range and type-specific keys; explicit delivery guarantees (at least once, deduplicated, unordered); duplicate detection by set membership instead of "seq greater than last"; metrics validated against the device type; payload limit 1024 bytes; contract split into platform, metric definitions and application parts. |
