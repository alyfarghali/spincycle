#pragma once

#include <stdint.h>
#include "esp_err.h"
#include "freertos/FreeRTOS.h"
#include "freertos/queue.h"
#include "config.h"

esp_err_t mqtt_task_start(const device_config_t *cfg, uint32_t boot, QueueHandle_t windows);
