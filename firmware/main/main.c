#include "esp_event.h"
#include "esp_log.h"
#include "esp_netif.h"
#include "freertos/FreeRTOS.h"
#include "freertos/queue.h"
#include "freertos/task.h"
#include "nvs_flash.h"
#include "sdkconfig.h"
#include "accel.h"
#include "secrets.h"
#include "sensor_task.h"
#include "time_sync.h"
#include "wifi_manager.h"

static const char *TAG = "main";

void app_main(void)
{
    esp_log_level_set("wifi", ESP_LOG_WARN);  // the driver is chatty at INFO; hides our own lines
    ESP_ERROR_CHECK(nvs_flash_init());  // Wi-Fi driver stores calibration data here
    ESP_ERROR_CHECK(esp_netif_init());
    ESP_ERROR_CHECK(esp_event_loop_create_default());

    ESP_ERROR_CHECK(wifi_manager_start(DEV_WIFI_SSID, DEV_WIFI_PASSWORD));
    if (!wifi_manager_wait_connected(30000)) {
        ESP_LOGW(TAG, "no Wi-Fi after 30 s; still retrying in the background");
    }
    // After Wi-Fi: a request sent before there's an IP fails and SNTP waits ~30 s to retry.
    time_sync_start();

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
