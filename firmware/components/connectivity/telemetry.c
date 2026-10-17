// TODO: build the telemetry payload. docs/telemetry-contract.md A4 is the source of truth.
//
// int telemetry_build(char *buf, size_t len, const char *device_id, const char *fw,
//                     uint32_t boot, uint64_t seq, const struct timeval *ts, float rms);
// Returns bytes written, or -1 if it doesn't fit in len or rms isn't finite.
//
// Rules:
//   v          always 1
//   seq        uint64: print with PRIu64 (<inttypes.h>); %d / %lu are wrong on some targets
//   ts         "2026-10-20T14:03:11.250Z" (UTC, ms, Z) via gmtime_r + tv_usec / 1000;
//              ts == NULL -> "ts":null (clock not synced yet)
//   metrics    {"vibration_rms": ...}, >= 4 significant digits; NaN/inf aren't valid JSON
//   size       <= 1024 bytes
//
// snprintf, not a JSON library: fixed tiny payload, no dependency, compiles on the host for tests.
// cJSON is for parsing config messages, not here.
//
// Tests (test/test_telemetry.c): known ts -> exact string; NULL ts -> null; NAN -> -1;
// buffer too small -> -1, never a truncated payload; seq > 2^32 prints correctly.

