# test

Host-side unit tests for the pure firmware code. Plain C with `assert`, built with the Mac's `cc`
(no ESP-IDF, no board), warnings as errors.

```sh
firmware/test/run.sh          # builds and runs all of them; stops at the first failure
```

| Test | Code | Spec it checks |
|---|---|---|
| `test_rms.c` | `sensing/rms.c` | Contract B1: still → 0 flat, tilted, on its side and with a 3 % sensor offset; a sine of amplitude A → A/√2 along any mounting axis; doubling A doubles RMS; non-whole periods at the real 102.3 Hz rate; Gaussian noise of σ → σ; `n = 0` → NaN (rejected downstream) |
| `test_telemetry.c` | `connectivity/telemetry.c` | Contract A4: the whole payload byte for byte; `ts` UTC with ms and `Z` (including a leap day and 999999 µs truncating, not rolling over); `null` before sync; `boot` up to `UINT32_MAX`, `seq` up to 2^53; ≥ 4 significant digits; NaN and ±∞ rejected; exact-fit buffer works, one byte short fails; worst case ≤ 1024 bytes |
| `test_backoff.c` | `connectivity/backoff.c` | Contract A2: 1, 2, 4 … 32 s then 60 s; never overflows; ±20 % jitter bounds; different random values give different delays |

`compile_flags.txt` (git-ignored, per machine) tells clangd these files target macOS and where the headers are.

Proven able to fail: 2-digit milliseconds, 3 significant digits, a 64 s cap, ±10 % jitter and skipping
gravity removal each fail a test.

Hardware code (`accel.c`, `sensor_task.c`, `wifi_manager.c`, `time_sync.c`) is tested on the board by
`firmware/test_app/`.
