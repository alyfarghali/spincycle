# Firmware

ESP-IDF v6.1 project for the ESP32-C6 sensor node. It samples an ICM-20948 accelerometer at ~102 Hz,
reduces each ~5 s window to one `vibration_rms` value, and publishes it over MQTT/TLS
in the format defined by `docs/telemetry-contract.md`.

Status legend: **done** = implemented and tested · **stub** = one-line placeholder, not written yet.

## How the pieces fit

```
accel (I2C, FIFO) ──samples──▶ sensor_task ──rms_window()──▶ queue of window_t ──▶ mqtt_task
                                                                                      │
              wifi_manager (connect + backoff)   telemetry_build() (JSON) ◀───────────┤
                                                 offline_queue (while disconnected) ◀─┤
                                                                                      ▼
                                                                                    broker
```

Two FreeRTOS tasks connected by a queue: sampling never waits on the network, and a Wi-Fi outage
never stops sampling.

## Layout

```
firmware/
  CMakeLists.txt         project file
  sdkconfig.defaults     committed ESP-IDF settings
  main/                  app_main: startup order only
  components/
    sensing/             accelerometer driver, RMS math, sensor task
    connectivity/        Wi-Fi, SNTP, MQTT, telemetry payload, offline queue
    device_config/       Device settings (from secrets.h for now) and the NVS boot counter
  test/                  host-side unit tests for the pure code
  test_app/              on-board tests (separate ESP-IDF project)
  tools/                 provisioning and recording scripts (stubs)
```

Each component exposes only the headers in its `include/` folder. Everything else in the component
(helpers marked `static`, `secrets.h`) is private to it.

---

## Top level

| File | Purpose |
|---|---|
| `CMakeLists.txt` | ESP-IDF project file: pulls in the IDF build system and names the image `spincycle`. |
| `sdkconfig.defaults` | Committed settings: target `esp32c6`; ESP-IDF's TLS certificate bundle (used to verify the broker's certificate); the "single app large" partition table (1.5 MB app, because TLS and MQTT outgrow the default 1 MB). `sdkconfig` itself is generated from it and git-ignored: delete it after changing the defaults. |

## `main/`

| File | Purpose |
|---|---|
| `main.c` | `app_main`, the entry point ESP-IDF calls after boot: startup order only. |
| `CMakeLists.txt` | Registers `main.c`. |

### `main.c` — `app_main(void)`
Startup sequence, in order:
1. Quiets the Wi-Fi driver's `"wifi"` log tag to warnings.
2. `nvs_flash_init()`, `esp_netif_init()`, `esp_event_loop_create_default()`: shared system setup.
3. `config_load()`, then `config_next_boot()`. A failure is fatal: publishing with a reused boot number would make storage drop readings as duplicates.
4. `wifi_manager_start()` then `wifi_manager_wait_connected(30000)`; on timeout it logs and continues (Wi-Fi keeps retrying in the background). Then `time_sync_start()`.
5. `accel_init()`, an 8-slot `window_t` queue, `mqtt_task_start()`, `sensor_task_start()`.

---

## `components/sensing/`

`CMakeLists.txt` registers `accel.c rms.c sensor_task.c`, requires the I2C and GPIO drivers.
`Kconfig.projbuild` adds a "SpinCycle sensor" menu: `SPINCYCLE_I2C_SDA_GPIO` (6), `SPINCYCLE_I2C_SCL_GPIO` (7),
`SPINCYCLE_ACCEL_I2C_ADDR` (0x69). These must match `hardware/wiring.md`.

### `accel.h` / `accel.c` — ICM-20948 driver · **done**
Types and constants:
- `accel_sample_t { float x, y, z; }` — one sample in g.
- `ACCEL_ODR_NUM / ACCEL_ODR_DEN` (1125/11 ≈ 102.3 Hz) — the real sample rate, kept as a fraction so it works in array sizes. `ACCEL_ODR_HZ` is the float form.

Public:
| Function | Does |
|---|---|
| `esp_err_t accel_init(int sda, int scl)` | Checks both I²C lines, creates the I²C bus at 100 kHz, confirms the chip (`WHO_AM_I` = 0xEA), resets and wakes it, sets ±2 g / ~102 Hz / ~50 Hz low-pass filter, enables the accel-only FIFO. Returns `ESP_ERR_NOT_FOUND` if the chip doesn't answer (after logging an address scan). |
| `esp_err_t accel_read(accel_sample_t *out)` | Reads the current x/y/z registers directly. Used for the bench sanity check, not for windows. |
| `esp_err_t accel_fifo_reset(void)` | Empties the FIFO and clears any overflow flag. |
| `esp_err_t accel_fifo_read(accel_sample_t *out, size_t max, size_t *got)` | Copies up to `max` (and at most 64) whole samples out of the FIFO, sets `*got`. Returns `ESP_ERR_INVALID_STATE` if the FIFO overflowed, meaning samples were lost. |

Private (`static`): `write_reg`, `read_regs` (one I²C register write / burst read); `check_line` (logs whether a line is pulled up, floating or shorted — wiring diagnosis); `diagnose_bus` (scans all addresses when the chip doesn't answer); `decode` (6 big-endian bytes → three floats in g).

### `rms.h` / `rms.c` — window maths · **done**, tested
| Function | Does |
|---|---|
| `float rms_window(const float *x, const float *y, const float *z, size_t n)` | Contract B1: per-sample magnitude √(x²+y²+z²), subtract the window mean (removes gravity and orientation), return √(mean of squared deviations). Equivalent to the population standard deviation of the magnitudes. Pure: no hardware, host-testable. |

Private: `magnitude(x, y, z)`.

### `window.h` — shared result type
- `window_t { uint64_t seq; float rms; struct timeval ts; bool ts_valid; }` — one window's result, passed from `sensor_task` to whoever publishes it.
- Separate from `sensor_task.h` so pure code (offline queue, host tests) can use it without FreeRTOS headers.

### `sensor_task.h` / `sensor_task.c` — sampling task · **done**
| Function | Does |
|---|---|
| `esp_err_t sensor_task_start(QueueHandle_t out, uint16_t interval_s)` | Validates arguments (1–60 s), starts the `sensor` FreeRTOS task (4 KB stack, priority 5). Each window's `window_t` is sent to `out`. |

Task behaviour (`sensor_task`, private):
- Window size is a sample count: `interval_s × 1125/11` (511 samples ≈ 5.0 s at 5 s).
- Drains the FIFO every 250 ms (`collect_window`); the sensor's own clock paces sampling, so task timing jitter doesn't matter.
- FIFO overflow → restarts the window (it no longer covers continuous time). No samples within 2× the window length → window skipped, but its `seq` is still used so the gap is visible server-side.
- Timestamps the window end with `gettimeofday`; `ts_valid` is true only once the clock is past 2025-01-01 (i.e. SNTP has synced).
- Never blocks on the queue: if it's full the window is dropped and logged, so a stalled publisher can't stop sampling.

Sample buffers (`s_x/s_y/s_z`, ~72 KB for the 60 s maximum) are `static`, not on the task stack.

---

## `components/connectivity/`

`CMakeLists.txt` registers every file below. Headers expose `window.h` and `config.h`, so `sensing` and `device_config` are public dependencies; `esp_wifi esp_netif esp_event esp_timer mbedtls` and esp-mqtt are private.
`idf_component.yml` pins `espressif/mqtt`, which isn't part of ESP-IDF since v6; the build downloads it into `managed_components/`.

### `wifi_manager.h` / `wifi_manager.c` — station Wi-Fi · **done**, reconnect test pending
| Function | Does |
|---|---|
| `esp_err_t wifi_manager_start(const char *ssid, const char *pass)` | Creates the event group and retry timer, the default station netif, initialises the Wi-Fi driver, registers event handlers, copies credentials (size-limited), and starts connecting. Returns immediately; errors are returned, not fatal. Requires NVS, netif and the default event loop to be initialised first (done in `main.c`). |
| `bool wifi_manager_wait_connected(uint32_t timeout_ms)` | Blocks the calling task until an IP address is held or the timeout passes. Returns whether it's connected. Doesn't consume the flag, so several tasks can wait. |

Private:
- `on_event` — on start: connect. On disconnect: clear the connected flag, log the reason code, schedule a retry. On got-IP: log the IP, reset the backoff, set the flag.
- `retry_cb` — `esp_timer` callback that calls `esp_wifi_connect()`. A timer is used because the event handler runs on the shared event task and must not block.
- Log tag is `wifi_mgr`, not `wifi`, because `main.c` silences `wifi` to warnings.

### `backoff.h` / `backoff.c` — reconnect delay · **done**, tested
| Function | Does |
|---|---|
| `uint32_t backoff_ms(uint32_t attempt, float r)` | Delay before reconnect attempt `attempt`: 1, 2, 4, 8, 16, 32, then 60 s (contract A2), times `0.8 + 0.4 r` for ±20 % jitter. `r` is uniform in [0, 1); `wifi_manager` passes `esp_random()`. Pure, so it's host-tested. |

### `telemetry.h` / `telemetry.c` — payload builder · **done**, tested
| Function | Does |
|---|---|
| `int telemetry_build(char *buf, size_t len, const char *device_id, const char *fw, uint32_t boot, uint64_t seq, const struct timeval *ts, float rms)` | Writes the contract A4 JSON (`v`, `device_id`, `fw`, `boot`, `seq`, `ts`, `metrics.vibration_rms`) into `buf`. `ts == NULL` → `"ts":null`. Returns the length, or `-1` if `rms` isn't finite or the message doesn't fit in `len` (never a truncated payload). |

Private: `format_ts(tv, out)` — UTC `YYYY-MM-DDTHH:MM:SS.mmmZ` via `gmtime_r`; returns false if it didn't fit.
`device_id` and `fw` aren't JSON-escaped because the contract limits IDs to `[a-z0-9-]`.

### `offline_queue.h` / `offline_queue.c` · **done**, tested
Fixed 60-slot ring buffer of `window_t` (no `malloc`) for readings taken while disconnected.
| Function | Does |
|---|---|
| `void offline_queue_init(offline_queue_t *q)` | Empty the queue. |
| `bool offline_queue_push(offline_queue_t *q, const window_t *w)` | Add a window, dropping the oldest when full. Returns false for a window without a valid `ts` (contract: null-ts readings aren't queued). |
| `bool offline_queue_peek(const offline_queue_t *q, window_t *out)` | Copy the oldest window without removing it. |
| `void offline_queue_pop(offline_queue_t *q)` | Remove the oldest. Peek → publish → pop means a failed publish keeps the reading. |

### `time_sync.h` / `time_sync.c` — clock sync · **done**
| Function | Does |
|---|---|
| `void time_sync_start(void)` | Starts ESP-IDF's SNTP client against `pool.ntp.org`, which keeps the clock corrected in the background. Logs `clock synced` on first sync. Call it after Wi-Fi connects: a request sent before there's an IP fails, and the retry comes ~30 s later. |
| `bool time_sync_now(struct timeval *out)` | Fills `out` with the current time; returns false until the clock has synced (before then it reads 1970), so callers send `ts: null`. |

### `mqtt_task.h` / `mqtt_task.c` — broker connection and publishing · **done**, not yet run on the board
| Function | Does |
|---|---|
| `esp_err_t mqtt_task_start(const device_config_t *cfg, uint32_t boot, QueueHandle_t windows)` | Copies the config, builds the three topics, starts the esp-mqtt client (contract A2: MQTT 3.1.1, client ID and username = device ID, keepalive 30 s, clean session, Last Will `offline` QoS 1 retained, TLS verified against the certificate bundle including the hostname), then starts the `mqtt_pub` task (4 KB stack, priority 5). |

Private:
- `on_event` — on connect: reset the backoff, publish `online`, subscribe to `config`, and only then allow telemetry (A2 order). On disconnect: schedule a retry with `backoff_ms`. On error: log why (refused credentials or the TLS error). On config data: log it, not applied (below).
- `retry_cb` — timer callback that reconnects. esp-mqtt's automatic reconnect is off because it uses a fixed delay.
- `publish_window` — builds the payload (`ts: null` until the clock is valid) and publishes at QoS 1. Returns false if it should be retried; a reading that can't be encoded is dropped.
- `flush_offline` — peek, publish, pop, oldest first, while connected.
- `mqtt_task` — for each window: flush the queue, then publish, or queue it if disconnected. A disconnected window without a valid `ts` is dropped (A5). The flush runs when the next window arrives, so within one period of reconnecting.

Remote config (A7) isn't implemented; it's on the project's cut list. The device subscribes and logs `config`, and always uses a 5 s interval.

---

## `components/device_config/`

`CMakeLists.txt` registers `config.c`, requires `nvs_flash`.

### `config.h` / `config.c` · **done**
- `device_config_t` — `device_id`, `fleet_id`, `mqtt_pass`, `broker_url`, `wifi_ssid`, `wifi_pass` (sized to contract maxima + NUL), `interval_s`.

| Function | Does |
|---|---|
| `esp_err_t config_load(device_config_t *out)` | Fills the config from `secrets.h`, with a 5 s interval. Reading NVS instead (written by `tools/provision.py`) comes when there's more than one device. |
| `esp_err_t config_next_boot(uint32_t *boot)` | Increments the boot counter (NVS namespace `spincycle`, key `boot`, starting at 1) and commits it **before the first publish** (contract A4), so a crash can't reuse a boot number. |

**Don't run `idf.py erase-flash` on a deployed device.** It resets the boot counter, so new readings reuse old `(boot, seq)` pairs and storage drops them as duplicates. `idf.py flash` keeps NVS.

### `secrets.h.example` / `secrets.h`
Wi-Fi credentials and the device's identity: `DEV_DEVICE_ID`, `DEV_FLEET_ID`, `DEV_MQTT_URL`, `DEV_MQTT_PASSWORD` (from `broker/dynsec/device.sh add`). Copy the example to `secrets.h` (git-ignored). One device per build until provisioning exists.

---

## `test/` — host tests

`firmware/test/run.sh` builds and runs every host test on the Mac. What each covers is in `test/README.md`.

## `test_app/` — on-board tests

A separate ESP-IDF project that runs Unity tests on the real board: the pure code on the chip's own libc and
soft float, the accelerometer, Wi-Fi, SNTP and `sensor_task`. `firmware/test_app/run.sh <port>`; details
in `test_app/README.md`.

## `tools/` · **stubs**

| File | Will do |
|---|---|
| `provision.py` | Write device ID, fleet ID, MQTT password, broker URL and Wi-Fi credentials into NVS over serial. |
| `record_cycle.py` | Log RMS lines from serial into a `testdata/cycles/` CSV (contract C1) while a machine runs. Use the board's UART socket. |

## Build and flash

```sh
get_idf                       # alias for . ~/.espressif/tools/activate_idf_v6.1.sh
cd firmware
idf.py build
idf.py -p /dev/cu.usbserial-XXXX flash monitor     # Ctrl+] to exit
idf.py -B build.clang -D IDF_TOOLCHAIN=clang reconfigure   # refresh clangd after adding files
```
