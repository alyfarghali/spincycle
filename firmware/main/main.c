// app_main: load config, start Wi-Fi, SNTP, sensor and MQTT tasks, arm the task watchdog.
//
// TEMPORARY sensor bench test: no Wi-Fi/MQTT. Prints raw samples, then one RMS per window.
// Replace with the real startup sequence once wifi_manager and mqtt_task are wired up.
#include "esp_log.h"
#include "freertos/FreeRTOS.h"
#include "freertos/queue.h"
#include "freertos/task.h"
#include "sdkconfig.h"
#include "accel.h"
#include "sensor_task.h"

static const char *TAG = "main";

void app_main(void)
{
    ESP_ERROR_CHECK(accel_init(CONFIG_SPINCYCLE_I2C_SDA_GPIO, CONFIG_SPINCYCLE_I2C_SCL_GPIO));

    // 1. Raw samples: lying flat and still, expect about x=0, y=0, z=+1 g.
    for (int i = 0; i < 20; i++) {
        accel_sample_t s;
        if (accel_read(&s) == ESP_OK) {
            ESP_LOGI(TAG, "raw x=%+.3f y=%+.3f z=%+.3f g", s.x, s.y, s.z);
        } else {
            ESP_LOGW(TAG, "raw read failed");
        }
        vTaskDelay(pdMS_TO_TICKS(250));
    }

    // 2. Windows: still -> rms near 0 (noise floor); shake or tap -> clearly higher.
    QueueHandle_t windows = xQueueCreate(8, sizeof(window_t));
    ESP_ERROR_CHECK(sensor_task_start(windows, 5));

    for (;;) {
        window_t w;
        if (xQueueReceive(windows, &w, portMAX_DELAY) == pdTRUE) {
            ESP_LOGI(TAG, "window seq=%llu rms=%.5f g ts_valid=%d", w.seq, w.rms, w.ts_valid);
        }
    }
}
