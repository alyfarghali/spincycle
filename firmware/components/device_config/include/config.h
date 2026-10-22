#pragma once

#include <stdint.h>
#include "esp_err.h"

typedef struct {
    char device_id[33];
    char fleet_id[33];
    char mqtt_pass[65];
    char broker_url[128];
    char wifi_ssid[33];
    char wifi_pass[65];
    uint16_t interval_s;
} device_config_t;

esp_err_t config_load(device_config_t *out);
esp_err_t config_next_boot(uint32_t *boot);
esp_err_t config_set_interval(uint16_t s);
