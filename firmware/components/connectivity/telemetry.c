#include "telemetry.h"

#include <inttypes.h>
#include <math.h>
#include <stdbool.h>
#include <stdio.h>
#include <time.h>

#define TS_LEN 25

static bool format_ts(const struct timeval *tv, char out[TS_LEN]) {
    struct tm t;
    time_t secs = tv->tv_sec;
    gmtime_r(&secs, &t);

    int n = snprintf(out, TS_LEN, "%04d-%02d-%02dT%02d:%02d:%02d.%03dZ", t.tm_year + 1900,
        t.tm_mon + 1, t.tm_mday, t.tm_hour, t.tm_min, t.tm_sec, (int)(tv->tv_usec / 1000));
    return n > 0 && n < TS_LEN;
}

int telemetry_build(char *buf, size_t len, const char *device_id,
    const char *fw, uint32_t boot, uint64_t seq, const struct timeval *ts, float rms) {
    if (!isfinite(rms)) {
        return -1;
    }

    char ts_json[TS_LEN + 2];
    if (ts == NULL) {
        snprintf(ts_json, sizeof ts_json, "null");
    } else {
        char temp[TS_LEN];
        if (!format_ts(ts, temp)) {
            return -1;
        }
        snprintf(ts_json, sizeof ts_json, "\"%s\"", temp);
    }

    int n = snprintf(buf, len,
        "{\"v\":1,\"device_id\":\"%s\",\"fw\":\"%s\",\"boot\":%" PRIu32
        ",\"seq\":%" PRIu64 ",\"ts\":%s,\"metrics\":{\"vibration_rms\":%.6g}}",
        device_id, fw, boot, seq, ts_json, rms
    );

    if (n < 0 || (size_t) n >= len) {
        return -1;
    }
    return n;
}
