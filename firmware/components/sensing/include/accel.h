#pragma once

#include <stddef.h>
#include "esp_err.h"

// Output data rate set in accel_init: 1125 / (1 + 10) Hz. Not exactly 100.
// Kept as a fraction so integer constant expressions (array sizes) can use it.
#define ACCEL_ODR_NUM 1125
#define ACCEL_ODR_DEN 11
#define ACCEL_ODR_HZ  ((float)ACCEL_ODR_NUM / ACCEL_ODR_DEN)

typedef struct {
    float x, y, z; // g
} accel_sample_t;

esp_err_t accel_init(int sda_gpio, int scl_gpio);
esp_err_t accel_read(accel_sample_t *out);

// FIFO: the sensor's own clock paces samples; the caller just drains it often enough
// (512-byte FIFO at ~102 Hz x 6 bytes fills in ~0.8 s, so drain at least every ~0.5 s).
esp_err_t accel_fifo_reset(void);
// Copies up to max whole samples out of the FIFO into out and sets *got.
// Returns ESP_ERR_INVALID_STATE if the FIFO overflowed (samples lost; caller should reset).
esp_err_t accel_fifo_read(accel_sample_t *out, size_t max, size_t *got);
