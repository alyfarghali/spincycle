#include "esp_event.h"
#include "esp_log.h"
#include "esp_netif.h"
#include "freertos/FreeRTOS.h"
#include "freertos/queue.h"
#include "nvs_flash.h"
#include "sdkconfig.h"
#include "accel.h"
#include "config.h"
#include "mqtt_task.h"
#include "sensor_task.h"
#include "time_sync.h"
#include "wifi_manager.h"

static const char *TAG = "main";

void app_main(void)
{
    esp_log_level_set("wifi", ESP_LOG_WARN);  // the driver is chatty at INFO; hides our own lines
    ESP_ERROR_CHECK(nvs_flash_init());  // the boot counter and the Wi-Fi driver's calibration data live here
    ESP_ERROR_CHECK(esp_netif_init());
    ESP_ERROR_CHECK(esp_event_loop_create_default());

    device_config_t cfg;
    ESP_ERROR_CHECK(config_load(&cfg));
    // Fatal on failure: publishing with a reused boot number would make storage drop readings as duplicates.
    uint32_t boot;
    ESP_ERROR_CHECK(config_next_boot(&boot));
    ESP_LOGI(TAG, "%s boot %lu", cfg.device_id, (unsigned long)boot);

    ESP_ERROR_CHECK(wifi_manager_start(cfg.wifi_ssid, cfg.wifi_pass));
    if (!wifi_manager_wait_connected(30000)) {
        ESP_LOGW(TAG, "no Wi-Fi after 30 s; still retrying in the background");
    }
    // After Wi-Fi: a request sent before there's an IP fails and SNTP waits ~30 s to retry.
    time_sync_start();

    ESP_ERROR_CHECK(accel_init(CONFIG_SPINCYCLE_I2C_SDA_GPIO, CONFIG_SPINCYCLE_I2C_SCL_GPIO));

    QueueHandle_t windows = xQueueCreate(8, sizeof(window_t));
    ESP_ERROR_CHECK(mqtt_task_start(&cfg, boot, windows));
    ESP_ERROR_CHECK(sensor_task_start(windows, cfg.interval_s));
}
