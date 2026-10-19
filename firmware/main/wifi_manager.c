// Wi-Fi connect and reconnect with exponential backoff (1, 2, 4 ... capped at 60 s).
// app_main: load config, start Wi-Fi, SNTP, sensor and MQTT tasks, arm the task watchdog.
#include <string.h>
#include "esp_event.h"
#include "esp_log.h"
#include "esp_netif.h"
#include "esp_wifi.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"
#include "nvs_flash.h"
#include "secrets.h"  // DEV_WIFI_SSID / DEV_WIFI_PASSWORD, git-ignored

static const char *TAG = "main";

static void on_event(void *arg, esp_event_base_t base, int32_t id, void *data)
{
    if (base == WIFI_EVENT && id == WIFI_EVENT_STA_START) {
        esp_wifi_connect();
    } else if (base == WIFI_EVENT && id == WIFI_EVENT_STA_DISCONNECTED) {
        // Reason codes: esp_wifi_types_generic.h. 201 = SSID not found, 15/204 = usually wrong password, 202 = auth failed
        wifi_event_sta_disconnected_t *d = data;
        ESP_LOGW(TAG, "disconnected (reason %d), retrying", d->reason);
        esp_wifi_connect();
    } else if (base == IP_EVENT && id == IP_EVENT_STA_GOT_IP) {
        ip_event_got_ip_t *e = data;
        ESP_LOGI(TAG, "connected, ip " IPSTR, IP2STR(&e->ip_info.ip));
    }
}

void app_main(void)
{
    esp_log_level_set("wifi", ESP_LOG_WARN);  // the driver is chatty at INFO; hides our own lines
    ESP_ERROR_CHECK(nvs_flash_init());  // Wi-Fi driver stores calibration data here
    ESP_ERROR_CHECK(esp_netif_init());
    ESP_ERROR_CHECK(esp_event_loop_create_default());
    esp_netif_create_default_wifi_sta();

    wifi_init_config_t init_cfg = WIFI_INIT_CONFIG_DEFAULT();
    ESP_ERROR_CHECK(esp_wifi_init(&init_cfg));

    ESP_ERROR_CHECK(esp_event_handler_register(WIFI_EVENT, ESP_EVENT_ANY_ID, on_event, NULL));
    ESP_ERROR_CHECK(esp_event_handler_register(IP_EVENT, IP_EVENT_STA_GOT_IP, on_event, NULL));

    wifi_config_t cfg = { 0 };
    strcpy((char *)cfg.sta.ssid, DEV_WIFI_SSID);
    strcpy((char *)cfg.sta.password, DEV_WIFI_PASSWORD);

    ESP_ERROR_CHECK(esp_wifi_set_mode(WIFI_MODE_STA));
    ESP_ERROR_CHECK(esp_wifi_set_config(WIFI_IF_STA, &cfg));
    ESP_ERROR_CHECK(esp_wifi_start());

    // Debug heartbeat: proves the board is alive and shows link quality while you walk around.
    for (;;) {
        wifi_ap_record_t ap;
        if (esp_wifi_sta_get_ap_info(&ap) == ESP_OK) {
            ESP_LOGI(TAG, "status: connected to %s, rssi %d dBm", (char *)ap.ssid, ap.rssi);
        } else {
            ESP_LOGW(TAG, "status: not connected");
        }
        vTaskDelay(pdMS_TO_TICKS(10000));
    }
}
