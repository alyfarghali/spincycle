#include <assert.h>
#include <math.h>
#include <stdio.h>
#include <string.h>
#include "telemetry.h"

static char buf[256];

static int build(uint64_t seq, const struct timeval *ts, float rms, size_t len)
{
    return telemetry_build(buf, len, "c6-washer-03", "0.1.0", 12, seq, ts, rms);
}

int main(void)
{
    struct timeval tv = {1792504991, 250000}; // date -u -r 1792504991 → 2026-10-20 14:03:11

    assert(build(1842, &tv, 0.0423f, sizeof buf) > 0);
    assert(strstr(buf, "\"ts\":\"2026-10-20T14:03:11.250Z\""));
    puts(buf);

    assert(build(1842, NULL, 0.0423f, sizeof buf) > 0);
    assert(strstr(buf, "\"ts\":null"));

    assert(build(5000000000ULL, NULL, 0.0423f, sizeof buf) > 0);
    assert(strstr(buf, "\"seq\":5000000000"));

    assert(build(1842, NULL, NAN, sizeof buf) == -1);
    assert(build(1842, NULL, 0.0423f, 20) == -1);

    puts("ok");
}
