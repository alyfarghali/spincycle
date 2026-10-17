#pragma once

#include "esp_err.h"

typedef struct {
    float x, y, z; // g
} accel_sample_t;

esp_err_t accel_init(int sda_gpio, int scl_gpio);
esp_err_t accel_read(accel_sample_t *out);
