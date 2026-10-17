#pragma once

#include <stdbool.h>
#include <stdint.h>
#include "esp_err.h"

esp_err_t wifi_manager_start(const char *ssid, const char *pass);
bool wifi_manager_wait_connected(uint32_t timeout_ms);
