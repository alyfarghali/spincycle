# SpinCycle

**Live laundry machine availability for campus laundry rooms, powered by ESP32-C6 sensors and a general-purpose IoT fleet backend.**

> 🚧 Work in progress: target launch **Nov 8, 2026**. The backend is deployed at
> **[spincycle.alyfarghali.com](https://spincycle.alyfarghali.com)** (broker on `:8883`); the web page is next.

### Status

| Part | State |
|---|---|
| Firmware | Wi-Fi with backoff, SNTP, accelerometer and RMS on the board; MQTT publishing next |
| Broker | Deployed: TLS on 8883 with a Let's Encrypt certificate, per-device accounts (dynsec) |
| Ingest | Deployed: validation, dedup, TimescaleDB, state engine, Redis live state |
| API | Contract written ([`docs/api.md`](docs/api.md)); skeleton in [`services/api/`](services/api/) |
| Web page | Not started |
| Load generator | 10,000 separate device connections; benchmark results pending |

Never walk down to the laundry room just to find every machine taken. SpinCycle puts a small vibration sensor on each washer and dryer, figures out which machines are free, running or finished, and shows it on a live web page. Tap **"Notify me"** and you'll get an email when your load is done.

Under the hood, SpinCycle is a general IoT fleet platform: devices, device types and fleets are modeled generically, so a laundry room is just the first fleet, not the only one.

---

## Features

- **Live machine status**: free, running or finished, updated every few seconds
- **"Notify me when done"**: email alert when your machine finishes
- **Busy-times chart**: when the laundry room is usually quiet, by hour and day
- **Device management**: per-device credentials, heartbeats and offline alerts
- **Built to scale**: load-tested with 10,000 simulated devices (results below)

## Architecture

```mermaid
flowchart LR
    subgraph Device["Per machine: hardware + firmware"]
        ACC["Accelerometer"] -->|"I²C, 100 Hz"| FW["ESP32-C6 firmware<br/>RMS every 5 s"]
    end
    subgraph Tools["Tools"]
        LG["Load generator<br/>10k simulated devices"]
        DM["Demo replayer<br/>recorded cycles"]
    end
    FW -->|"MQTT/TLS :8883<br/>telemetry, status, LWT"| MQ["Mosquitto 2.x<br/>dynsec auth + ACL"]
    MQ -->|"retained config"| FW
    LG --> MQ
    DM --> MQ
    MQ -->|"$share/ingest/..."| ING["Ingest service (Go)<br/>decode, dedupe, batch write,<br/>state engine, offline sweeper"]
    ING -->|"COPY batches,<br/>machine_events"| PG[("Postgres 16 + TimescaleDB<br/>durable record")]
    ING -->|"live state, dedup,<br/>events pub/sub"| RD[("Redis 7<br/>cache only")]
    RD --> API["API service (Python, FastAPI)<br/>REST, notifier, busy-times"]
    PG <--> API
    API -->|"dynsec commands,<br/>device config"| MQ
    WEB["React page"] -->|"HTTPS, poll 5 s"| CAD["Caddy"]
    CAD -->|"/api"| API
    API -->|"finished / offline alerts"| USR["Students + team"]
    ING -.->|"/metrics"| PROM["Prometheus"]
    API -.->|"/metrics"| PROM
```

1. Each ESP32-C6 samples an accelerometer at 100 Hz and publishes one vibration RMS value every 5 seconds over MQTT/TLS.
2. Mosquitto authenticates each device and only lets it publish to its own topics in its own fleet.
3. The ingest service validates and dedupes readings, batch-writes them to TimescaleDB, updates live state in Redis, and runs the state engine that turns vibration into free / running / finished / offline.
4. The API serves live state from Redis and history from TimescaleDB to the web page, notifies students when their machine finishes, and alerts the team when a device goes offline.

## Low-level design

Each high-level component is a top-level folder; each low-level piece is a file or subfolder inside it.

### Hardware — [`hardware/`](hardware/) (Twyla)

```mermaid
flowchart LR
    OUT["Wall outlet"] --> PSU["USB adapter, 5 V<br/>power.md"]
    PSU --> C6["ESP32-C6 dev board"]
    C6 <-->|"I²C SDA/SCL, 3.3 V<br/>wiring.md"| ACC["Accelerometer<br/>sensor.md<br/>LIS3DH / ADXL345 / LIS2DH12"]
    ACC --- MNT["Magnet mount, top panel<br/>mount.md"]
    MNT --- MACH["Washer / dryer"]
    C6 --- ENC["Enclosure<br/>enclosure/"]
    ACC --- ENC
```

Sensor choice, power source and mount are still open; `bom.md` tracks cost per device and `pcb/` is the stretch custom board.

### Firmware — [`firmware/`](firmware/) (Twyla)

```mermaid
flowchart TB
    NVS[("config.c<br/>NVS: ids, creds, broker URL,<br/>boot counter, interval")] --> MAIN["main.c<br/>app_main + task watchdog"]
    MAIN --> WIFI["wifi_manager.c<br/>backoff 1, 2, 4 … 60 s"]
    MAIN --> SNTP["time_sync.c<br/>SNTP"]
    MAIN --> ST["sensor_task.c<br/>5 s windows"]
    MAIN --> MT["mqtt_task.c"]
    ACC["accel.c<br/>I²C, 100 Hz"] --> ST
    ST -->|"500 samples"| RMS["rms.c<br/>magnitude − running mean → RMS"]
    RMS -->|"FreeRTOS queue"| MT
    MT --> TEL["telemetry.c<br/>device_id, boot, seq, ts, metrics"]
    SNTP -->|"ts or null"| TEL
    MT <-->|"while disconnected"| OQ["offline_queue.c<br/>~60 readings, drop oldest"]
    MT -->|"QoS 1, TLS with CA verified<br/>online status + LWT"| BROKER["Mosquitto"]
    BROKER -->|"retained config"| MT
    PROV["tools/provision.py"] -.->|"serial"| NVS
    REC["tools/record_cycle.py"] -.->|"serial → CSV"| CSV["testdata/cycles/"]
```

### MQTT broker — [`broker/`](broker/) (Aly)

```mermaid
flowchart LR
    DEV["Device<br/>user = device ID"] -->|"publish telemetry, status"| ACL{"dynsec role<br/>own topics, own fleet"}
    ACL -->|"allowed"| TOP["fleets/{fleet}/devices/{id}/…<br/>topics.md"]
    ACL -->|"denied"| DROP["rejected"]
    TOP -->|"$share/ingest"| ING["ingest account"]
    APIA["api account"] -->|"retained config"| TOP
    APIA -->|"$CONTROL/dynamic-security"| DS["Dynsec plugin<br/>dynsec/bootstrap.sh, roles.md"]
    LGA["loadgen account"] -->|"fleets/loadtest/… only"| TOP
    CADDY["Caddy's cert"] -->|"certs/sync-certs.sh"| TLS["mosquitto.conf<br/>TLS :8883, wss fallback"]
```

### Ingest service — [`services/ingest/`](services/ingest/) (Aly, Go)

```mermaid
flowchart LR
    MQ["Mosquitto"] -->|"telemetry + status"| SUB["subscriber/<br/>paho MQTT 5, persistent<br/>session, manual ack"]
    SUB -->|"chan, cap 10k"| DEC["decoder/<br/>validate, fleet matches topic"]
    REG["registry/<br/>refresh every 60 s"] --> DEC
    DEC -->|"status / LWT"| SW["sweeper/<br/>every 30 s"]
    DEC --> DD["dedup/<br/>(boot, seq)"]
    DD --> BW["batchwriter/<br/>500 rows or flush interval"]
    DD --> LS["livestate/"]
    DD --> EN["engine/<br/>pure state machine"]
    SW --> EN
    BW -->|"COPY"| TEL[("telemetry")]
    BW -->|"after commit"| ACK["subscriber/acker.go<br/>in-order PUBACK"]
    ACK --> MQ
    LS --> RLS[("Redis latest, last_seen")]
    EN --> RUN["engine/runner.go"]
    RUN --> RST[("Redis state + events")]
    RUN --> ME[("machine_events")]
```

The acker is what makes QoS 1 survive an ingest crash: a message is only acknowledged after the row holding it is committed.

#### State engine — [`services/ingest/engine/`](services/ingest/engine/)

```mermaid
stateDiagram-v2
    [*] --> free
    free --> running: shaking for start_confirm
    finished --> running: shaking for start_confirm
    running --> finished: quiet for stop_confirm
    finished --> free: clear_after elapsed
    free --> offline: silent for offline_after, or LWT
    running --> offline: silent for offline_after, or LWT
    finished --> offline: silent for offline_after, or LWT
    state "restore previous state + timers" as resume
    offline --> resume: back within resume_window
    offline --> unknown: back after resume_window
    unknown --> running: shaking for start_confirm
    unknown --> free: quiet for start_confirm
```

There is deliberately no `offline → free` edge: a sensor that drops off Wi-Fi mid-cycle must never show its machine as free. Thresholds and timers come from `device_types.config` / `devices.config`, and `replay_test.go` runs every recorded cycle in `testdata/cycles/` through the engine.

### Storage — [`storage/`](storage/) (Aly)

```mermaid
erDiagram
    fleets ||--o{ devices : contains
    device_types ||--o{ devices : configures
    devices ||--o{ telemetry : reports
    devices ||--o{ machine_events : transitions
    devices ||--o{ subscriptions : "watched by"
    fleets ||--o{ busy_times : "summarized in"
    telemetry ||--o{ telemetry_hourly : "continuous aggregate"
    fleets {
        text id PK
        text name
    }
    device_types {
        text id PK
        jsonb metrics
        jsonb config "thresholds and timers"
    }
    devices {
        text id PK
        text fleet_id FK
        text type_id FK
        text kind "washer or dryer"
        jsonb config "per-device overrides"
        timestamptz deleted_at "soft delete"
    }
    telemetry {
        timestamptz time "hypertable, 90-day retention"
        text device_id
        text metric
        float8 value
    }
    telemetry_hourly {
        timestamptz bucket
        text device_id
        text metric
        float8 avg_value
        float8 max_value
    }
    machine_events {
        bigserial id PK
        text device_id FK
        text from_state
        text to_state
        timestamptz at
    }
    subscriptions {
        uuid id PK
        text device_id FK
        text contact "nulled after alert"
        text status "pending, sending, sent, expired, failed"
    }
    busy_times {
        text fleet_id
        int weekday
        int hour
        float8 avg_running
    }
```

```mermaid
flowchart LR
    ING["Ingest"] -->|"writes"| K1["device:{id}:latest / last_seen / lastseq"]
    ING -->|"writes"| K2["device:{id}:state"]
    ING -->|"publishes"| EV["events channel"]
    APIW["API"] -->|"writes"| K3["fleet:{id}:devices<br/>ratelimit:sub:{ip}"]
    K1 --> APIR["API reads for the page"]
    K2 --> APIR
    EV --> NOT["Notifier worker"]
```

Redis is a cache with persistence off; [`storage/redis/keys.md`](storage/redis/keys.md) documents how each key rebuilds if it is wiped.

### API service — [`services/api/`](services/api/) (Aly, Python)

```mermaid
flowchart TB
    CAD["Caddy /api"] --> MW["middleware/<br/>adminauth, ratelimit"]
    MW --> H["routes/<br/>machines, busytimes, subscriptions,<br/>admin_devices, health"]
    H --> STO["store/<br/>postgres, redis"]
    H -->|"register, update, revoke"| DEVS["devices/<br/>credentials, dynsec, configpush"]
    DEVS --> MQ["Mosquitto"]
    EV[("Redis events")] --> W["notifier/worker.py<br/>asyncio background task"]
    CUR["notifier/cursor.py<br/>persisted catch-up over machine_events"] --> W
    W -->|"running → finished"| CL["notifier/claim.py<br/>pending → sending"]
    CL --> N["Notifier Protocol<br/>Web Push recommended, open"]
    N -->|"ok"| SENT["sent, contact nulled"]
    N -->|"fail"| RT["notifier/retry.py<br/>3 retries, then failed"]
    W -->|"any → offline"| OA["notifier/offline_alerts.py<br/>1 h suppression"]
    EXP["notifier/expiry.py<br/>pending > 4 h → expired"]
    BT["busytimes/job.py<br/>nightly"] --> BTT[("busy_times")]
```

### Web page — [`web/`](web/) (Aly; Twyla writes How it works)

```mermaid
flowchart TB
    R["main.tsx router"] --> FP["/ and /f/{fleet}<br/>FleetPage"]
    R --> MP["/m/{id}<br/>MachinePage, QR target"]
    R --> CP["CancelPage"]
    R --> HW["HowItWorks"]
    FP --> MG["MachineGrid"]
    MG --> MC["MachineCard<br/>text label + time in state"]
    FP --> BTC["BusyTimesChart<br/>7 × 24 heatmap"]
    MP --> MC
    MP --> ND["NotifyDialog"]
    MG --> UP["usePolling<br/>5 s, paused when tab hidden"]
    UP --> CLI["api/client.ts"]
    ND --> CLI
    CP -->|"POST cancel"| CLI
    CLI -.->|"unreachable"| AB["ApiBanner"]
```

### Load generator and demo — [`tools/`](tools/) (Aly; demo shared)

```mermaid
flowchart LR
    SEED["loadgen/seed.py<br/>register loadtest devices"] --> LMAIN["loadgen/main.go<br/>flags"]
    LMAIN --> RAMP["ramp.go<br/>linear ramp"]
    RAMP --> VD["device.go × N<br/>idle → 35 min wash → idle"]
    VD --> POOL["pool.go<br/>N devices over M connections"]
    POOL -->|"fleets/loadtest/…"| MQ["Mosquitto"]
    MQ --> ING["Ingest"]
    POOL -->|"published/s"| REP["report.go"]
    ING -.->|"/metrics: stored/s, p50, p99"| REP
    REP --> LT["docs/load-tests.md"]
    CSV["testdata/cycles/*.csv"] --> DEMO["demo/replay.py"]
    DEMO -->|"fleets/demo/…"| MQ
```

### Deployment, CI and observability — [`deploy/`](deploy/), [`.github/workflows/`](.github/workflows/) (Aly; Twyla owns the firmware build)

```mermaid
flowchart TB
    subgraph VM["Shared portfolio server"]
        EDGE["edge Caddy :80, :443<br/>HTTPS for every project"]
        subgraph SC["SpinCycle: docker-compose.yml + docker-compose.prod.yml"]
            CAD["proxy: Caddy :80 internal<br/>Caddyfile"] --> API["api"]
            MQ["mosquitto :8883"] --> ING["ingest"]
            ING --> PG[("postgres + timescaledb<br/>named volume")]
            ING --> RD[("redis, no persistence")]
            API --> PG
            API --> RD
        end
        EDGE -->|"spincycle.alyfarghali.com"| CAD
        EDGE -->|"TLS cert, sync-certs.sh"| MQ
    end
    LAPTOP["Laptop"] -->|"deploy.sh: rsync, compose up --build"| SC
    PG -->|"backup/pg_dump.sh, nightly"| BK["Off-box backup"]
    UPT["External uptime check"] -->|"/health"| EDGE
```

SpinCycle shares a server with other projects; one edge Caddy terminates HTTPS for all of them.
[`deploy/README.md`](deploy/README.md) has the layout and setup.

## Tech stack

| Layer | Technology |
| --- | --- |
| Firmware | ESP-IDF (C) on ESP32-C6, ICM-20948 accelerometer |
| Messaging | MQTT (Mosquitto) |
| Backend | Python (FastAPI) for the API, notifier and tooling; Go for the ingest service and load generator |
| Storage | PostgreSQL + TimescaleDB, Redis |
| Frontend | React |
| Infrastructure | Docker Compose, GitHub Actions |

## Repository structure

One top-level folder per high-level component; every file starts with a one-line comment saying what it is for.

```
spincycle/
├── hardware/            # Sensor, power, mount, wiring, enclosure, BOM, optional PCB
├── firmware/            # ESP-IDF project for the ESP32-C6
│   ├── main/            # accel, sensor_task, rms, mqtt_task, telemetry, offline_queue,
│   │                    # wifi_manager, time_sync, config, secrets.h.example
│   ├── test/            # Host-side tests for pure firmware code
│   └── tools/           # provision.py, record_cycle.py
├── broker/              # mosquitto.conf, topics, dynsec setup and roles, cert sync
├── services/
│   ├── ingest/          # Go: subscriber, decoder, registry, dedup, batchwriter,
│   │                    # livestate, engine, sweeper, metrics
│   └── api/             # Python: routes, middleware, store, devices, notifier, busytimes
├── storage/             # Postgres migrations, seed data, Redis key layout
├── web/                 # React page: pages, components, hooks, API client
├── tools/
│   ├── loadgen/         # Go: simulated device fleet for benchmarks
│   └── demo/            # Replays recorded cycles into the demo fleet
├── testdata/cycles/     # Recorded washer/dryer cycles (shared fixtures)
├── deploy/              # docker-compose.yml, Caddyfile, Prometheus, backups, deploy script
├── docs/                # Telemetry contract, provisioning, load-test results, ADRs
├── .github/workflows/   # CI, deploy, firmware build
├── go.mod               # Single Go module for ingest and loadgen
└── .env.example
```

## Getting started

**Prerequisites:** Docker and Docker Compose. For firmware: [ESP-IDF](https://docs.espressif.com/projects/esp-idf/) and an ESP32-C6 board.

```bash
git clone https://github.com/alyfarghali/spincycle.git
cd spincycle
cp .env.example .env
broker/certs/make-dev-certs.sh
docker compose --env-file .env -f deploy/docker-compose.yml up -d postgres redis mosquitto
broker/dynsec/bootstrap.sh
docker compose --env-file .env -f deploy/docker-compose.yml up -d --build ingest
```

Set every `change-me` in `.env` first. Each component's README covers the rest:
[`firmware/`](firmware/README.md), [`services/ingest/`](services/ingest/README.md),
[`services/api/`](services/api/README.md), [`storage/`](storage/README.md),
[`tools/loadgen/`](tools/loadgen/README.md), [`deploy/`](deploy/README.md).

## Message format

Devices publish to:

```
fleets/{fleet_id}/devices/{device_id}/telemetry
```

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

`boot` increments on every restart and `seq` on every message, so `(device_id, boot, seq)` identifies a
reading: the backend stores each one once even when MQTT delivers it twice or out of order. Full spec: [`docs/telemetry-contract.md`](docs/telemetry-contract.md).

## Benchmarks

_Coming in week 4._

| Simulated devices | Messages/sec | p99 ingest latency | Hardware |
| --- | --- | --- | --- |
| 10,000 | TBD | TBD | TBD |

## Roadmap

- [ ] **Week 1:** First real sensor reading reaches the backend
- [ ] **Week 2:** Machine state detected on a real washer; status page v1
- [ ] **Week 3:** Deployed with HTTPS; devices installed in the laundry room
- [ ] **Week 4:** Email notifications, offline alerts, load test, launch

**Later:** web push notifications, "almost done" estimates, a second fleet on the same backend, offline buffering, OTA firmware updates.

## Privacy

SpinCycle never uses cameras and stores no personal data on devices. Emails entered for "Notify me" are used only for that alert and then deleted.

## Team

- **[Your name](https://github.com/<you>)**: firmware, ingest pipeline, device management
- **[Friend's name](https://github.com/<friend>)**: API, web page, notifications, deployment

## License

MIT
