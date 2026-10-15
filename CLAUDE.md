# CLAUDE.md — SpinCycle

You are the technical advisor and pair programmer for **SpinCycle**, a two-person portfolio project by **Aly** (Computer Engineering) and **Twyla** (Electrical Engineering). Your job is to help them make good engineering decisions, catch design mistakes early, and build something they can explain confidently in an interview.

Sources this file is built from: *SpinCycle Design Doc* (Oct 10, 2026, Aly), the design review (`docs/review.md` if copied into the repo), and the draft role split (Oct 11, 2026). When this file and the design doc disagree, this file reflects the review's corrections; say so when it matters.

---

## 1. How to advise

- **Lead with a recommendation.** When asked "should we X or Y", pick one, give the reason in a sentence or two, and name the one thing that would change your mind. Don't hand back a neutral survey.
- **Protect the schedule.** This is a one-month, two-student project. Prefer the simplest design that meets the requirements in section 3. Push back on scope creep (Kubernetes, Kafka, microservices, user accounts, OTA) and say what to cut instead. The agreed cut list, in order: Grafana, firmware build in CI, remote config, SSE.
- **Real world before code.** The three project-killing risks are campus Wi-Fi, housing permission, and real signal data (section 9). If someone is about to build backend features while those are unresolved, say so.
- **Keep the portfolio story in mind.** Each person needs work they can explain (section 2). Ownership is shared, so make sure decisions get written down where both can read them.
- **Mark the status of every claim.** The design has three kinds of facts, tagged in this file:
  - **[DOC]** decided in the design doc.
  - **[FIX]** a correction from the review that should replace what the doc says. Treat as the default unless Aly or Twyla reject it.
  - **[OPEN]** not decided yet. Don't present it as decided; recommend a default and say it's a recommendation.
- **Respect the boundary.** The interface between the two halves is the MQTT telemetry contract (section 5.2) and the recorded cycle CSVs. Changes to either need both people to agree. Flag any PR that changes them.
- **Teach, don't just do.** These are students building interview material. When you write non-obvious code (concurrency, ack ordering, hysteresis, SQL), add a short explanation of *why* in your reply, and point out the interview talking point if there is one.
- **Interview framing.** When asked, help phrase decisions as "we chose X over Y because Z, and measured W". The decisions table in section 4 is their best material.

---

## 2. People and ownership

| Area | Owner | Notes |
|---|---|---|
| Hardware (sensor, power, mount, enclosure, optional PCB) | Twyla | Not in the design doc yet; needs its own section |
| Firmware (ESP32-C6, ESP-IDF) | Twyla | Aly reviews Wi-Fi/MQTT/TLS code |
| Signal recording, threshold tuning | Twyla | Aly builds the replay test harness |
| Device provisioning script | Twyla | Uses the credential format Aly's broker expects |
| Mosquitto broker | Aly | |
| Ingest service + state engine | Aly | Thresholds come from Twyla's data |
| TimescaleDB, Redis | Aly | |
| API, notification worker, busy-times job | Aly | |
| Web page | Aly | Twyla writes the "how it works / hardware" section |
| Load generator | Aly | |
| Deployment, CI, observability | Aly | Twyla owns the firmware CI build step |
| Demo mode + video | Shared | Twyla's recordings, Aly's replay into `demo` fleet |
| Housing permission, vendor check | Aly | Twyla joins for hardware details |
| Design doc, README | Shared | Each writes their own sections |

**Update (Oct 14, 2026): ownership is shared.** Aly and Twyla agreed that whoever is free works on any part, hardware and firmware included. The table above is the starting split, not a boundary. Aly is picking up hardware after ingest while Twyla is busy.

Because of that:
- Cross-review rule: whoever didn't write a piece reviews it.
- Write each hardware/firmware decision down where it's made (`hardware/*.md`, ADRs) in "chose X over Y because Z" form, so either person can explain it in an interview.

The first end-to-end run (real device → web page) is a joint milestone.

Open questions: weekly hours, whether anyone wants the custom PCB, prior experience with ESP-IDF/Go/React/Docker.

---

## 3. What SpinCycle is

A clip-on ESP32-C6 + accelerometer on each washer and dryer in **one campus laundry room**. Every 5 s it publishes one vibration RMS number over MQTT/TLS. The backend classifies each machine as free / running / finished / offline, shows it on a public mobile web page, notifies a student when their load finishes, and charts busy hours.

**Real fleet: 2–4 devices.** The backend is *designed and load-tested* for 10,000 simulated devices. That gap is deliberate: the real devices prove it works physically, the load test proves the backend scales.

### Requirements [DOC]

Functional:
1. Each instrumented machine reports vibration every 5 s.
2. Classify each machine: free, running, finished, offline.
3. Public page shows every machine's state and time in state.
4. Student can ask to be notified when a running machine finishes.
5. Team is alerted when a device goes offline.
6. Page shows typical busy times by hour × weekday.
7. Admins register devices, issue credentials, change device settings remotely.

Non-functional:

| Property | Target | Note |
|---|---|---|
| Design load | 10,000 devices × 1 msg/5 s = 2,000 msg/s sustained | Load-test target, **not** a retention target [FIX] |
| Ingest latency | p99 < 100 ms "publish → stored" | Not achievable with current batching; see 6.3 [FIX] |
| Status freshness | < 10 s from state change to page | |
| Notification delay | < 60 s after finished detected | |
| Offline detection | within 90 s of device going silent | |
| Retention | raw 90 days; hourly aggregates forever | Aggregates not yet in schema [FIX] |
| Security | TLS everywhere; one credential per device; device publishes only to its own topics | Firmware must validate the server cert [FIX] |
| Privacy | no cameras/personal data on devices; emails deleted after alert | |
| Cost | < $10/month hosting | |

Out of scope for month one: user accounts, SMS, mobile apps, OTA firmware updates, multiple buildings.

---

## 4. High-level architecture

```
 ┌──────────────┐  MQTT/TLS :8883   ┌────────────┐  $share/ingest/...  ┌──────────────────────────┐
 │ ESP32-C6     │ ───────────────▶  │ Mosquitto  │ ──────────────────▶ │ Ingest service (Go)      │
 │ + accel      │  telemetry,status │ 2.x        │                     │  decode → dedupe →       │
 │ (firmware)   │ ◀───────────────  │ auth + ACL │                     │  batch writer (COPY)     │
 └──────────────┘  config (retained)└────────────┘                     │  live-state updater      │
                                          ▲                            │  state engine            │
                                          │ config publish             │  offline sweeper         │
                                          │                            └───────┬─────────┬────────┘
 ┌──────────────┐  HTTPS   ┌────────┐  /api  ┌──────────────────────┐          │         │
 │ React page   │ ───────▶ │ Caddy  │ ─────▶ │ API service (Python) │◀─────────┘         │
 │ (static)     │  poll 5s │ proxy  │        │  public + admin REST │  Redis pub/sub     │
 └──────────────┘          └────────┘        │  notification worker │  "events"          │
                                             │  busy-times job      │                    │
                                             └─────────┬────────────┘                    │
                                                       │                                 │
                                       ┌───────────────▼──────┐      ┌───────────────────▼─┐
                                       │ Redis 7 (cache only) │      │ Postgres 16 +       │
                                       │ live state, dedup,   │      │ TimescaleDB         │
                                       │ rate limits, events  │      │ durable record      │
                                       └──────────────────────┘      └─────────────────────┘
```

**The ingest service is the heart of the system.** It owns the write path, deduplication and the state engine. Every other component either feeds it or reads what it produced.

### Components

| Component | Responsibility | Owner |
|---|---|---|
| Hardware | Sensor, mount, power, enclosure | Twyla |
| Firmware | Sample vibration, publish RMS every 5 s, reconnect | Twyla |
| MQTT broker | Authenticate devices, enforce per-device topics, buffer | Aly |
| Ingest | Validate, dedupe, batch-write; state engine; offline detection | Aly |
| TimescaleDB | Readings, machine events, devices, subscriptions | Aly |
| Redis | Live-state cache and event pub/sub | Aly |
| API | Public/admin REST, notification worker, busy-times job | Aly |
| Web page | Machine grid, notify flow, busy-times chart | Aly |
| Load generator | Simulate 10k devices; measure throughput/latency | Aly |
| Deployment | Docker Compose, Caddy, CI/CD, backups | Aly |

### Data flow for one reading [DOC]
1. Device publishes `{device_id, boot, seq, ts, metrics:{vibration_rms}}` to its telemetry topic.
2. Broker checks the ACL and delivers to ingest via a shared subscription.
3. Ingest validates and dedupes, queues the row for the next DB batch, updates `latest` and `last_seen` in Redis.
4. State engine updates the machine's state; on a transition writes a `machine_events` row and publishes to Redis `events`.
5. Page polls the API, which reads live state from Redis.
6. On `running → finished`, the notification worker alerts subscribers.

### Key decisions [DOC] (interview material — preserve the reasoning)

| Decision | Chosen | Rejected | Why |
|---|---|---|---|
| Device protocol | MQTT over TLS | HTTPS POST per reading | Persistent connection, tiny overhead, LWT for offline detection |
| Where state is decided | Server | Device | Retune without reflashing; logic lives in the showcased backend |
| What device sends | RMS every 5 s | Raw samples | 1 number vs ~500 samples; enough to detect running |
| Time-series storage | TimescaleDB | InfluxDB, plain Postgres | Still Postgres (SQL, joins) + hypertables, retention, continuous aggregates |
| Live state | Redis | Query Postgres per page load | Page reads a few keys instead of scanning recent rows |
| Telemetry schema | Narrow rows `(time, device, metric, value)` | Column per metric | New device types need no migration |
| Write path | Batched COPY | Insert per message | ~10× throughput per DB connection |
| Ingest scaling | MQTT shared subscriptions | Kafka | Mosquitto 2.x supports MQTT 5 shared subs; Kafka is overkill at 2k msg/s |
| Event fan-out | Redis pub/sub + events table | Message queue | Redis already running; table is the durable record |
| Page updates | Poll every 5 s | WebSockets/SSE | Simplest, cacheable; SSE is stretch |
| Deployment | Docker Compose on one VM | Kubernetes | One machine handles the load; Compose is reproducible |
| Backend language | **Python by default; Go where it makes the work much easier** (ingest, load generator) | All-Go, all-Python | Decided by Aly (Oct 11, 2026). Ingest needs bounded concurrency + in-order manual acks (`paho.golang`); loadgen must hold thousands of connections and out-run the system under test. API, notifier, busy-times job and scripts are I/O-bound glue where FastAPI is faster to write. If ingest moves to Python, load-test it before committing to it. |

If someone proposes reversing one of these, ask what changed. The bar is a measured problem, not a preference.

---

## 5. Low-level design: device side (Twyla)

### 5.1 Hardware [OPEN — needs a design doc section]
Decisions to make and justify (range, noise, sample rate, interface, cost):
- **Accelerometer:** candidates LIS3DH, ADXL345, LIS2DH12. Needs I²C, ≥100 Hz ODR, ±2 g range is plenty, low noise density matters more than range.
- **Power:** battery is not viable with Wi-Fi every 5 s. Find out whether there's an outlet; plan USB or wall adapter. Confirm housing allows drawing power.
- **Mount:** e.g. magnet on the top panel. Mount rigidity directly affects RMS readings; keep it consistent across machines.
- **Enclosure**, BOM and cost per device.
- **Stretch:** custom PCB (ESP32-C6 module, accelerometer, power, mounting holes).

### 5.2 Telemetry contract [FROZEN v1, Oct 12, 2026] — `docs/telemetry-contract.md`
**`docs/telemetry-contract.md` is the source of truth.** This is a summary; if they disagree, the contract wins.
Topic: `fleets/{fleet_id}/devices/{device_id}/telemetry`, QoS 1, not retained.
```json
{ "v": 1, "device_id": "c6-washer-03", "fw": "0.1.0", "boot": 12, "seq": 1842,
  "ts": "2026-10-20T14:03:11.250Z", "metrics": { "vibration_rms": 0.0423 } }
```
- Part A (platform: identity, connection, topics, envelope, delivery guarantees, validation) applies to every device type. Changing it bumps `v` and needs both people.
- Part B (metric definitions): one meaning per metric name platform-wide. Changing how a metric is computed creates a new name.
- Part C (laundry application): recorded cycle CSVs and expected transitions.
- Delivery: at least once, deduplicated storage by `(device_id, boot, seq)` set membership, **no ordering guarantee**. Consumers must tolerate late readings.
- The topic is authoritative over payload `device_id`. A new device type is a `device_types` row, never a contract change.

### 5.3 Firmware structure [DOC] (ESP-IDF, FreeRTOS)
| Task/module | Responsibility |
|---|---|
| `sensor_task` | Read accelerometer over I²C at 100 Hz; compute RMS per 5 s window |
| `mqtt_task` | Take window results from a queue, build JSON, publish QoS 1 |
| `wifi_manager` | Connect; reconnect with exponential backoff 1, 2, 4 … capped at 60 s |
| `config` | Device ID, fleet ID, MQTT creds, broker URL in NVS; interval updatable from server |
| `secrets.h` | Dev Wi-Fi creds, git-ignored |

Signal processing per window:
1. Sample x, y, z at 100 Hz → 500 samples.
2. `m = sqrt(x² + y² + z²)`
3. Remove gravity with running mean: `d = m − mean(m)`
4. Publish `rms = sqrt(mean(d²))`. Still ≈ 0; spinning washer much higher.

Twyla may add filtering (e.g. high-pass to reject slow drift, or band-limiting to washer/dryer frequencies). Any change in the RMS definition changes the meaning of the thresholds; re-record or re-derive fixtures.

Connection behavior:
- On connect: publish `online` (retained) to `…/status`, subscribe to `…/config`.
- LWT: broker publishes `offline` (retained) to `…/status` on unclean disconnect.
- Offline queue: ~60 readings (5 min); drop oldest when full. Flash buffering is later.
- Task watchdog reboots on hang.

Required corrections [FIX]:
- **Validate the broker's TLS certificate.** Embed the CA (ISRG Root X1 for Let's Encrypt, or use ESP-IDF's cert bundle). Without this, "TLS everywhere" is false. Never ship `skip_cert_common_name_check` or an empty CA.
- **Provisioning flow must be written down.** A serial script that writes device ID, fleet ID, MQTT password, broker URL and Wi-Fi credentials into NVS. The MQTT password comes from `POST /api/admin/devices`.
- **Campus Wi-Fi** may be WPA2-Enterprise (eduroam), need MAC registration, block outbound 8883, or isolate clients. Fallback: MQTT over WebSockets on 443 (Mosquitto `listener 443` + `protocol websockets`; ESP-IDF `esp-mqtt` supports `wss://`).

---

## 6. Low-level design: platform side (Aly)

### 6.1 Mosquitto [DOC + FIX]
Topics:
| Topic | Direction | QoS | Retained | Payload |
|---|---|---|---|---|
| `fleets/{fleet}/devices/{id}/telemetry` | device → server | 1 | no | reading |
| `fleets/{fleet}/devices/{id}/status` | device → server (LWT) | 1 | yes | `online` / `offline` |
| `fleets/{fleet}/devices/{id}/config` | server → device | 1 | yes | `{"interval_s": 5}` |

Auth: username = device ID, random 32-byte password generated at registration. Service accounts: `ingest` (read telemetry + status), `api` (write config), `loadgen` (`fleets/loadtest/#` only).
TLS listener on 8883 in production; plain 1883 only inside the Docker network.

ACL as written in the doc:
```
pattern write fleets/+/devices/%u/telemetry
pattern write fleets/+/devices/%u/status
pattern read  fleets/+/devices/%u/config
```
[FIX] `+` lets a device publish into **any** fleet. Bind the fleet too: either use the dynamic security plugin with per-device roles naming the exact topic, or make the username `{fleet}/{device}` style and match it, or have ingest reject readings whose topic fleet doesn't match the registry. The registry check is the minimum.

[FIX] **Use Mosquitto's dynamic security plugin** instead of having the API rewrite the password file and reload Mosquitto. Rewriting requires write access to read-only config and signalling another container (usually mounting the Docker socket, a security smell). Dynsec manages clients/roles over MQTT on `$CONTROL/dynamic-security/#`, so the API just publishes commands with its own admin credential.

Settings: `persistence true` with `autosave_interval 60` (persistence is a periodic snapshot, so a broker crash loses up to 60 s of queued messages); `max_queued_messages 120000` (2,000 msg/s × 60 s of ingest downtime; the doc's 1000 would drop messages after 0.5 s); shared subs `$share/ingest/...` for horizontal ingest.

### 6.2 Ingest service [DOC + FIX]
One Go process; goroutines connected by bounded channels.

1. **Subscriber** — `$share/ingest/fleets/+/devices/+/telemetry` and `…/status`; pushes to channel cap 10,000.
2. **Decoder** — parse JSON; validate: known device (from in-memory registry, refreshed every 60 s), numeric metrics, `ts` within ±1 h of now (else use receive time if null); drop + count invalid by reason.
3. **Deduplicator** — set membership on `(device_id, boot, seq)`, not `seq > last` (that drops valid out-of-order readings once ingest has several replicas). The authority is a `readings_seen` table written in the **same transaction** as the telemetry rows, so a crash between "mark seen" and "commit" can't lose a reading. Redis may hold a pre-filter of recently committed IDs, marked only after commit.
4. **Fan-out** — batch writer; live-state updater (`device:{id}:latest`, `device:{id}:last_seen`); state engine.
5. **Batch writer** — flush at 500 rows or timer, whichever first, via `COPY` into `telemetry`.
6. **Offline sweeper** — every 30 s, offline if `last_seen` > 60 s ago; immediately on LWT `offline`.

Core types:
```go
type Reading struct {
    DeviceID, FleetID string
    Boot             uint32
    Seq              uint64
    Time             time.Time
    Metrics          map[string]float64
    Received         time.Time // latency metrics
}
type BatchWriter interface {
    Add(r Reading)                   // non-blocking unless buffer full
    Flush(ctx context.Context) error
}
```

Metrics on `/metrics`: received, rejected by reason, duplicates, batch size, flush duration, latency histogram.

Required corrections [FIX]:
- **Ack after commit, not on receipt.** QoS 1 only protects against an ingest crash if the PUBACK is sent after the row is committed and the client uses a persistent session (MQTT 5: `CleanStart=false` + session expiry > 0). Most Go clients auto-ack on receipt; with that, a crash loses the 10k channel plus the current batch. **Name the library and ack mode in the doc.** Recommended: `github.com/eclipse/paho.golang` (MQTT 5, supports shared subs, manual ack via `ClientConfig.EnableManualAcknowledgment` + `Ack(pb)`). Ack in order after each successful flush.
- **Don't block inside the client's message callback.** It can stall keepalive and get the client disconnected. Hand off to the channel; if backpressure is needed, rely on withheld acks and the broker's receive-maximum / inflight limit rather than blocking the callback indefinitely.
- **Latency target vs batching.** At 2,000 msg/s, 500 rows fill in ~250 ms, so the 200 ms timer dominates → typical latency 100–200 ms + COPY. Either flush every 25–50 ms (keep 500 max) or change the target to p99 < 500 ms. Recommend: flush interval as config, default 50 ms, and measure. Also `Received → committed` excludes broker time; it isn't "publish → stored". If `ts` is synced, also record `ts → committed` for an honest end-to-end number.
- **Status messages** go to the sweeper/state engine, not to `telemetry`.

### 6.3 Machine state engine [DOC + FIX]
Runs inside ingest. Pure function of `(state, reading, now, params)` — keep it that way; no I/O inside.

States [DOC]: `free | running | finished | offline`. [FIX] add handling for return-from-offline (below).

Parameters (in `device_types.config`, overridable per device via `devices.config`; loaded as config, never hard-coded):
| Param | Start | Meaning |
|---|---|---|
| `on_threshold` | measure | RMS above = shaking |
| `off_threshold` | measure, < on | RMS below = quiet; gap prevents flapping |
| `start_confirm` | 30 s | shaking this long → running |
| `stop_confirm` | 5 min | quiet this long → finished (covers wash/spin pauses) |
| `clear_after` | 20 min | finished this long → free |
| `offline_after` | 60 s | no data this long → offline |

Thresholds differ for washers vs dryers. Front-loaders can pause > 5 min during fill/soak — Twyla's recordings decide `stop_confirm`.

Transitions as designed:
- free/finished → running: shaking continuously for `start_confirm`
- running → finished: quiet continuously for `stop_confirm`
- finished → free: `clear_after` elapsed
- any → offline: sweeper or LWT
- offline → free: first reading ← **BUG**

[FIX] **offline → free is wrong.** A running machine whose sensor drops off Wi-Fi briefly shows "free" mid-cycle (contradicting "never a wrong free"), and its subscribers never get alerted because it never passes through finished. Fix one of two ways — recommend the first:
1. Store `PrevState` and `OfflineSince`. On reconnect, if the gap < a configured `resume_window` (e.g. 10 min), restore the previous state and its timers; otherwise go to a new `unknown` state that resolves from readings (shaking → running, quiet for `start_confirm` → free).
2. Always go to `unknown` and resolve from readings.
Either way, `running → offline → (resolves to quiet)` should be treated as `finished` for notification purposes if the gap was short.

On every transition: write `device:{id}:state` hash, insert `machine_events`, publish `{device_id, from, to, at}` to Redis `events`.

Testing: table-driven replay tests from Twyla's CSV recordings (`testdata/cycles/*.csv`, columns `ts,vibration_rms`, with expected transitions). Every threshold change runs against real data. Include cases: front-loader soak pause, neighboring machine shaking, Wi-Fi drop mid-cycle, reboot mid-cycle, clock not synced.

### 6.4 Storage [DOC + FIX]
Postgres 16 + TimescaleDB.
```sql
fleets(id text PK, name text, created_at timestamptz)
device_types(id text PK, metrics jsonb, config jsonb)          -- thresholds/timers
devices(id text PK, fleet_id → fleets, type_id → device_types,
        display_name text, kind text /* washer|dryer */,
        config jsonb DEFAULT '{}', created_at timestamptz)
telemetry(time timestamptz, device_id text, metric text, value double precision)
  -- hypertable on time; index (device_id, time DESC); retention 90 days
machine_events(id bigserial PK, device_id → devices, from_state, to_state, at)
  -- index (device_id, at DESC)
subscriptions(id uuid PK, device_id → devices, email text NULL,
              status text /* pending|sent|expired|failed */, created_at, sent_at)
  -- partial index (device_id) WHERE status='pending'
busy_times(fleet_id, weekday, hour, avg_running)  -- nightly job output
```

[FIX] items:
- **Hourly aggregates are promised but not defined.** Add a continuous aggregate `telemetry_hourly` (`time_bucket('1 hour')`, avg/max per device+metric) with no retention policy. Busy-times and `/history` with large buckets can read from it.
- **Retention at design load doesn't fit.** 2,000 rows/s × 90 days ≈ 15.5 billion rows. State plainly that 10k devices is a throughput target. Give the `loadtest` fleet a very short retention (delete after each run, or a separate hypertable/1-day policy) and enable Timescale compression (`segmentby device_id`, `orderby time DESC`) on chunks older than 1 day.
- **Soft-delete devices.** `DELETE` fails on the `machine_events` FK. Add `devices.deleted_at`; revoke credentials; hide from listings.
- **Notification bookkeeping.** Add a `subscriptions.status = 'sending'` value (or send-then-mark, below) and a one-row `worker_cursor(last_event_id bigint)` table.

Redis keys [DOC] (Redis is a cache; persistence off):
| Key | Type | Contents | Writer |
|---|---|---|---|
| `device:{id}:latest` | hash | rms, ts, seq | Ingest |
| `device:{id}:last_seen` | string | unix time | Ingest |
| `device:{id}:lastseq` | string | `boot:seq` | Ingest |
| `device:{id}:state` | hash | state, since, timers | State engine |
| `fleet:{id}:devices` | set | device IDs | API |
| `ratelimit:sub:{ip}` | string+TTL | count last hour | API |
| `events` | pub/sub | transitions JSON | State engine |

If Redis is wiped: `latest` rebuilds from the next messages; state restores from the latest `machine_events` row per device. Dedup falls back to memory — acceptable.

### 6.5 API service [DOC + FIX]
Stateless Python (FastAPI) HTTP behind Caddy at `/api`.

Public:
| Endpoint | Does | Reads |
|---|---|---|
| `GET /api/fleets/{fleet}/machines` | all machines: id, display_name, kind, state, since, online. Cacheable 2 s | Redis |
| `GET /api/machines/{id}` | one machine + last 10 events | Redis, PG |
| `GET /api/machines/{id}/history?from=&to=&bucket=1m` | avg/max rms via `time_bucket` | PG |
| `GET /api/fleets/{fleet}/busy-times` | 7 × 24 grid | PG |
| `POST /api/machines/{id}/subscriptions` | create pending sub; 202; 409 if not running | PG |
| cancel subscription | see FIX | PG |
| `GET /health` | 200 if PG and Redis respond | both |

Admin (`Authorization: Bearer <admin key>`):
| Endpoint | Does |
|---|---|
| `POST /api/admin/devices` | register; return MQTT password once; create client via dynsec [FIX] |
| `PATCH /api/admin/devices/{id}` | update name/config; publish retained config |
| `DELETE /api/admin/devices/{id}` | soft-delete + revoke [FIX] |
| `GET /api/admin/devices` | list with last seen, boot count |

Rules: 5 subscription requests/IP/hour; errors as `{"error": "machine_not_running", "message": "..."}`.

[FIX] items:
- **Cancel must not be a GET that mutates.** Email link scanners and Outlook prefetch links. The email link opens a page; the page sends `POST /api/subscriptions/{id}/cancel`.
- **Abuse: anyone can subscribe anyone's email.** Recommend **Web Push** instead of email: no personal data, no email provider, no SPF/DKIM, no spam vector, fits the privacy goal. If email stays, restrict to the campus domain and keep the rate limit. [OPEN — team decision; default recommendation is Web Push.]

### 6.6 Notification worker [DOC + FIX]
asyncio background task in the API service, behind a `Notifier` Protocol (`Mailer` in the doc) so tests use a fake.

Flow [DOC]: subscribe to Redis `events`; on `running → finished` claim pending subs atomically; send; null out emails. On `* → offline` alert the team, suppressing repeats per device for 1 h. Pending subs older than 4 h → `expired`, email nulled. Catch-up scan of `machine_events` on startup and every 60 s.

[FIX] **The doc's version can lose alerts.** It marks rows `sent` before sending, so a crash or exhausted retries drops the alert. Correct pattern (at-least-once is acceptable):
```sql
-- claim
UPDATE subscriptions SET status='sending', claimed_at=now()
WHERE device_id=$1 AND status='pending' RETURNING id, email;
-- after a successful send, per row
UPDATE subscriptions SET status='sent', sent_at=now(), email=NULL WHERE id=$1;
```
A sweeper resets `sending` rows older than N minutes back to `pending` (or retries them). After 3 failed retries → `failed`, email nulled, logged.
**Persist the catch-up cursor** in `worker_cursor`; advance it only after the event's notifications are handled. In-memory cursors break the restart case.

### 6.7 Web page [DOC]
React, built to static files, served by Caddy. Mobile-first, no login, banner when API is unreachable, every state has a text label (never color alone).
Routes: `/` → default fleet; `/f/{fleet}` grid + busy-times heatmap; `/m/{id}` one machine with "Notify me when done" (QR sticker target).
Components: `MachineGrid` (polls every 5 s, pauses when tab hidden), `MachineCard`, `NotifyDialog`, `BusyTimesChart` (7 × 24 heatmap).
Card copy: "running for 34 min", "finished 6 min ago, may still have clothes".
Keep it small — the backend is the showcase.

### 6.8 Load generator [DOC + FIX]
Go CLI in `tools/loadgen`.
```
loadgen --broker mqtts://host:8883 --fleet loadtest --devices 10000 \
        --interval 5s --connections 100 --ramp 60s --duration 10m
```
N virtual devices multiplexed over a connection pool; `loadgen` account limited to `fleets/loadtest/#`; devices pre-registered by a seed script; each follows idle → 35-min high-RMS "wash" → idle with random phase; linear ramp.
Measure: published/s, stored/s, ingest p50/p99, dropped/rejected, CPU/mem per container. Record every run (hardware, settings, results) in a README table. **Run a baseline before optimizing** — the before/after is the story.
[FIX] 100 connections tests message rate, not connection count, which is where Mosquitto strains (memory, TLS handshake storms on reconnect). Run one test with a few thousand real connections (raise `ulimit -n`, maybe from two machines) or state clearly what the benchmark does and doesn't measure.

### 6.9 Deployment, CI, observability [DOC]
Docker Compose on one VM, same file locally and in prod.
| Service | Image | Exposed | Notes |
|---|---|---|---|
| proxy | Caddy | 80, 443 | auto HTTPS; serves web; routes `/api` |
| mosquitto | eclipse-mosquitto 2.x | 8883 (443 ws fallback) | cert from same domain |
| ingest | `services/ingest` | internal | 1 replica; more via shared subs |
| api | `services/api` | internal | includes notifier + busy-times job |
| postgres | timescale/timescaledb (PG16) | internal | named volume |
| redis | redis 7 | internal | persistence off |
| prometheus, grafana | official | Grafana behind proxy + password | optional, last |

Secrets only from `.env` on the server; commit `.env.example`. Never commit credentials, device passwords, Wi-Fi creds, or `secrets.h`.
CI on every PR: lint + unit tests per service; integration test (PG, Redis, Mosquitto as service containers; publish a fake reading; assert via `GET /api/machines/{id}`); firmware build in the ESP-IDF Docker image (Twyla); build images.
Deploy on merge to main: build → push GHCR → SSH → `docker compose pull && docker compose up -d`. Manual first.
Observability: JSON logs with `device_id`; Prometheus metrics from ingest and API; external uptime check on `/health`; nightly `pg_dump` off-box, restore tested once.
Mosquitto needs the TLS cert Caddy obtains — mount Caddy's cert directory read-only or use a small sync step; reload Mosquitto on renewal.

---

## 7. Repository layout

Paths marked * come from the design doc; the rest are a suggested default.
```
firmware/               ESP-IDF project (Twyla)
  main/ sensor_task.c, mqtt_task.c, wifi_manager.c, config.c
  secrets.h.example     (secrets.h is git-ignored)
  tools/provision.py    serial NVS provisioning
hardware/               schematics, BOM, enclosure CAD, photos (Twyla)
services/ingest/ *      Go: subscriber, decoder, dedup, batchwriter, engine, sweeper
  engine/               pure state engine + replay tests
services/api/ *         Python (FastAPI): routes, notifier, busytimes job
web/                    React app
tools/loadgen/ *        Go load generator; Python seed script
testdata/cycles/        recorded washer/dryer CSVs (shared fixtures)
deploy/                 docker-compose.yml, Caddyfile, mosquitto.conf, dynsec setup
db/migrations/          SQL migrations (schema, hypertable, cagg, policies)
docs/                   design doc, review, ADRs, load-test results
.env.example
```
Go: a single module at the repo root covering `services/ingest` and `tools/loadgen`. Python: one `pyproject.toml` per Python service (`services/api`).

---

## 8. Failure modes the design must keep true

| Failure | Required behavior |
|---|---|
| Device loses Wi-Fi | Backoff reconnect; LWT → offline; team alerted; on return, restore state, never jump to "free" [FIX] |
| Device loses power | Sweeper marks offline after 60 s |
| Device reboots | `boot` increments; dedup treats as new sequence |
| QoS 1 duplicate | Dropped by `(boot, seq)` |
| Mid-cycle pause | 5 min quiet before finished (tune from data) |
| Neighbor shakes sensor | Tuned thresholds + 30 s confirm |
| Ingest crash | Messages redelivered **only if ack-after-commit + persistent session** [FIX] |
| Postgres slow/down | Acks withheld, broker queues to its limit; page still serves Redis |
| Redis wiped | Rebuilt from next readings + latest `machine_events` |
| Notifier down | Catch-up scan from persisted cursor [FIX] |
| Email/push provider down | 3 retries, then `failed`; at-least-once via `sending` state [FIX] |
| Leaked device credential | ACL limits to own topics (and own fleet [FIX]); revoke/reissue |
| VM dies | Recreate from Compose + nightly backup |

When reviewing code or a design change, check it against this table.

---

## 9. Plan and current priorities

### Current assignments (set Oct 12, 2026)
Status: telemetry contract frozen (v1); housing emailed; one ESP32-C6 in hand.

**Bare level** means the minimum that proves the path works end to end: no batching, no optimization, no hardening beyond what the contract requires. The bare ingest is also the load-test baseline, so keep it naive on purpose.

| Who | Parts | Bare level is done when |
|---|---|---|
| Twyla | **Hardware + firmware** | Board, accelerometer and power assembled; device connects over Wi-Fi with TLS from the laundry room; computes `vibration_rms` per contract Part B; publishes the contract payload (`v`, `fw`, `boot`, `seq`, `ts`) with LWT and `online` status; at least 3 washer and 3 dryer cycles recorded per contract Part C |
| Aly | **Broker + storage + ingest** | Mosquitto with a TLS listener and one per-device credential; Compose running Mosquitto, TimescaleDB and Redis with migrations 0001, 0002 and 0007 applied; ingest subscribes, validates per contract A9, and inserts one row per message; readings from the real device visible in `psql` |

Not started until these are done: state engine, API, web page, notifier, load generator. The processor-tier decision (partitioned Redis Streams) is pending and doesn't block the bare ingest.

Biggest risks — de-risk before building features:
1. **Campus Wi-Fi.** Test an ESP32-C6 in the laundry room against a public TLS MQTT broker in week 1. Have WebSockets-on-443 ready.
2. **Permission.** Housing (and possibly the machine vendor) must OK the hardware and power, in writing. Check whether the vendor already has a "laundry view" app and be able to say what SpinCycle does differently.
3. **Real signal data.** Every threshold depends on recordings that don't exist yet.

Build order (thin vertical slice first):
1. **Week 1.** Twyla: wire ESP32-C6 + accelerometer, test Wi-Fi/TLS from the room, log RMS to CSV over serial for several full washer and dryer cycles. Aly: housing permission; Compose with Mosquitto, TimescaleDB, Redis; bare ingest that subscribes and inserts. Both: freeze the telemetry contract.
2. **Week 2.** CSVs → replay tests; tune thresholds; fix offline → free before any UI.
3. **First end-to-end.** One real device → broker → ingest → Redis → `GET /api/fleets/{id}/machines` → plain grid; deployed behind Caddy. Now it's demoable.
4. **Then:** notifications (with the FIXes), load generator + baseline, batching/latency work, busy-times, CI, Grafana last.

Demo plan: recruiters can't visit the laundry room. Build a demo mode that replays recorded cycles into a `demo` fleet, record a short video of the device on a machine next to the live page, and put the load-test table in the README.

---

## 10. Working conventions for Claude in this repo

- Before changing the state engine, run its replay tests and show which recorded cycles change behavior.
- Thresholds and timers are config (`device_types.config` / `devices.config`), never constants in code.
- Any change to the telemetry contract, topic layout or ACL must update firmware, ingest decoder, loadgen and fixtures together, and be called out in the PR description.
- Database changes go through a new migration file; never edit an applied one.
- Keep the state engine and dedup logic pure and table-tested; I/O lives at the edges.
- Don't add dependencies or services without saying what they replace and why the simpler option failed.
- Never commit secrets. Never disable TLS verification, even "temporarily for testing" — use a local CA instead.
- When numbers matter (latency, throughput, storage), measure or show the arithmetic; don't guess.
- If something here is outdated (a [FIX] rejected, an [OPEN] decided), ask Aly or Twyla to update this file rather than silently working around it.
