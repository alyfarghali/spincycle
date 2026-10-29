# test_app

On-board tests: a separate ESP-IDF project that reuses `../components` and runs Unity tests on the real
ESP32-C6 and ICM-20948. Flash it only when testing; the device firmware in `firmware/` is unchanged.

## Run

Board on the **UART** socket, lying flat and still, with a 2.4 GHz network in `secrets.h`:

```sh
get_idf
firmware/test_app/run.sh /dev/cu.usbserial-XXXX
```

It builds, flashes, prints one line per test, and exits 0 only if everything passed (~45 s).
For the full log instead: `cd firmware/test_app && idf.py -p /dev/cu.usbserial-XXXX flash monitor`.
Afterwards, flash the device firmware again from `firmware/`.

## Files

| File | Suite |
|---|---|
| `main/test_main.c` | Shared setup (NVS, netif, event loop) and the run order |
| `main/test_target.c` | target |
| `main/test_sensing.c` | accel, sensor_task |
| `main/test_connectivity.c` | connectivity |

**Status:** builds; not yet run on the board.

## Tests, in run order

Order matters: `accel_init` and `sensor_task_start` can each run once per boot, and the sensor task test
needs the clock synced first.

| Suite | Test | Checks |
|---|---|---|
| target | `test_telemetry_matches_contract_on_target` | Contract A4 payload byte for byte with the chip's own libc (picolibc), not the Mac's: `%.6g`, `PRIu64`, `gmtime_r`, leap day, ms truncation, NaN, short buffer |
| target | `test_rms_matches_contract_on_target` | Contract B1 on soft-float (the C6 has no FPU): sine A → A/√2 |
| target | `test_backoff_matches_contract_on_target` | Contract A2 delays and jitter floor |
| accel | `test_accel_init_finds_icm20948` | I²C wiring, address, `WHO_AM_I` |
| accel | `test_still_board_reads_one_g` | Mean magnitude 1 ± 0.05 g while still (scale factor, ±2 g range) |
| accel | `test_fifo_delivers_102_hz` | FIFO sample rate 1125/11 Hz ± 3 % over 4 s |
| accel | `test_fifo_overflow_is_reported_then_recovers` | Not draining for 1.5 s reports overflow; a reset recovers |
| connectivity | `test_wifi_connects` | Joins the `secrets.h` network within 30 s; the connected flag stays set |
| connectivity | `test_clock_syncs_after_wifi` | Clock invalid at boot, SNTP sync within 15 s, year ≥ 2026 |
| sensor_task | `test_sensor_task_rejects_bad_arguments` | NULL queue, interval 0 and 61 rejected |
| sensor_task | `test_sensor_task_windows` | Three windows: `seq` 0, 1, 2; ~5.0 s apart; RMS between 0.0002 and 0.005 g (still board); real timestamps |

## Failures and what they usually mean

| Failing test | Check first |
|---|---|
| `test_accel_init_finds_icm20948` | Qwiic plug seated at both ends, SDA/SCL on GPIO 6/7 (`hardware/wiring.md`) |
| `test_still_board_reads_one_g` | Board moving or not flat, or a wrong scale factor in `accel.c` |
| `test_fifo_delivers_102_hz` | Sample-rate divider or FIFO configuration in `accel_init` |
| `test_wifi_connects` | `secrets.h` values, 2.4 GHz network |
| `test_clock_syncs_after_wifi` | Network blocking NTP (UDP 123) |
| `test_sensor_task_windows` | Board moved during the test (RMS too high), or sensor stuck (RMS ~0) |

## Not covered here

Reconnect with backoff needs the network to drop: run the device firmware, turn the hotspot off for
~30 s and back on, and check `wifi_mgr` logs retries at ~1, 2, 4, 8 … s (± 20 %) and then reconnects,
while `main: window` lines keep coming (verified Oct 25, 2026).
