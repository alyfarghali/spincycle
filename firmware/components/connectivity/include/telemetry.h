#pragma once

#include <stddef.h>
#include <stdint.h>
#include <sys/time.h>

int telemetry_build(char *buf, size_t len, const char *device_id,
    const char *fw, uint32_t boot, uint64_t seq, const struct timeval *ts, float rms);
