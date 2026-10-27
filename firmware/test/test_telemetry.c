#include <assert.h>
#include <math.h>
#include <stdio.h>
#include <string.h>
#include "telemetry.h"

static char buf[2048];

static int build(uint64_t seq, const struct timeval *ts, float rms, size_t len)
{
    return telemetry_build(buf, len, "c6-washer-03", "0.1.0", 12, seq, ts, rms);
}

static void expect_ts(time_t secs, long usec, const char *want)
{
    struct timeval tv = {secs, usec};
    assert(build(1, &tv, 0.01f, sizeof buf) > 0);
    char field[64];
    snprintf(field, sizeof field, "\"ts\":\"%s\"", want);
    if (!strstr(buf, field)) {
        printf("want %s in %s\n", field, buf);
        assert(0);
    }
}

int main(void)
{
    // Contract A4: the whole payload, byte for byte.
    struct timeval tv = {1792504991, 250000}; // date -u -r 1792504991 → 2026-10-20 14:03:11
    int n = build(1842, &tv, 0.0423f, sizeof buf);
    const char *canonical = "{\"v\":1,\"device_id\":\"c6-washer-03\",\"fw\":\"0.1.0\",\"boot\":12,\"seq\":1842,"
                            "\"ts\":\"2026-10-20T14:03:11.250Z\",\"metrics\":{\"vibration_rms\":0.0423}}";
    puts(buf);
    assert(strcmp(buf, canonical) == 0);
    assert(n == (int)strlen(canonical));

    // ts: UTC, milliseconds, Z. Truncates microseconds rather than rounding into the next second.
    expect_ts(1735689600, 0, "2025-01-01T00:00:00.000Z");
    expect_ts(1792504991, 999999, "2026-10-20T14:03:11.999Z");
    expect_ts(1792504991, 1000, "2026-10-20T14:03:11.001Z");
    expect_ts(1835481599, 500000, "2028-02-29T23:59:59.500Z"); // leap day, last second

    // ts null before the clock syncs.
    assert(build(1842, NULL, 0.0423f, sizeof buf) > 0);
    assert(strstr(buf, "\"ts\":null"));

    // Integer ranges: boot is uint32, seq is uint64 (stays below 2^53, so safe as a JSON number).
    assert(telemetry_build(buf, sizeof buf, "a", "0", UINT32_MAX, 0, NULL, 0.01f) > 0);
    assert(strstr(buf, "\"boot\":4294967295"));
    assert(build(5000000000ULL, NULL, 0.0423f, sizeof buf) > 0);
    assert(strstr(buf, "\"seq\":5000000000"));
    assert(build(9007199254740992ULL, NULL, 0.0423f, sizeof buf) > 0);
    assert(strstr(buf, "\"seq\":9007199254740992"));

    // vibration_rms: at least 4 significant digits (contract B1), finite only.
    assert(build(1, NULL, 0.0001234f, sizeof buf) > 0);
    assert(strstr(buf, "\"vibration_rms\":0.0001234}"));
    assert(build(1, NULL, 0.0f, sizeof buf) > 0);
    assert(strstr(buf, "\"vibration_rms\":0}"));
    assert(build(1, NULL, NAN, sizeof buf) == -1);
    assert(build(1, NULL, INFINITY, sizeof buf) == -1);
    assert(build(1, NULL, -INFINITY, sizeof buf) == -1);

    // Never a truncated payload: exact fit works, one byte short fails.
    n = build(1842, &tv, 0.0423f, sizeof buf);
    assert(build(1842, &tv, 0.0423f, (size_t)n + 1) == n);
    assert(build(1842, &tv, 0.0423f, (size_t)n) == -1);
    assert(build(1842, &tv, 0.0423f, 20) == -1);

    // Worst case still fits the 1024-byte limit: 32-char ID, long fw, max boot and seq.
    n = telemetry_build(buf, sizeof buf, "abcdefghijklmnopqrstuvwxyz012345", "loadgen-10.20.30-rc.1",
                        UINT32_MAX, 9007199254740992ULL, &tv, 0.0001234f);
    assert(n > 0 && n <= 1024);

    puts("ok");
}
