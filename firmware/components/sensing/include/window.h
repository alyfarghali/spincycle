#pragma once

// Separate from sensor_task.h so pure code (offline_queue, host tests) doesn't pull in FreeRTOS.

#include <stdbool.h>
#include <stdint.h>
#include <sys/time.h>

typedef struct {
    uint64_t seq;
    float rms;
    struct timeval ts;
    bool ts_valid;
} window_t;
