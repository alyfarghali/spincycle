#pragma once

#include <stdint.h>
#include "esp_err.h"
#include "freertos/FreeRTOS.h"
#include "freertos/queue.h"
#include "window.h"

// Skipped windows (< 90 % of samples) are never sent but still consume a seq, so gaps are visible.
esp_err_t sensor_task_start(QueueHandle_t out, uint16_t interval_s);
