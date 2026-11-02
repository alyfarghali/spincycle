#include "config.h"

#include <string.h>
#include "esp_check.h"
#include "nvs.h"
#include "secrets.h"

static const char *TAG = "config";

esp_err_t config_load(device_config_t *out)
{
    strlcpy(out->device_id, DEV_DEVICE_ID, sizeof out->device_id);
    strlcpy(out->fleet_id, DEV_FLEET_ID, sizeof out->fleet_id);
    strlcpy(out->mqtt_pass, DEV_MQTT_PASSWORD, sizeof out->mqtt_pass);
    strlcpy(out->broker_url, DEV_MQTT_URL, sizeof out->broker_url);
    strlcpy(out->wifi_ssid, DEV_WIFI_SSID, sizeof out->wifi_ssid);
    strlcpy(out->wifi_pass, DEV_WIFI_PASSWORD, sizeof out->wifi_pass);
    out->interval_s = 5;
    return ESP_OK;
}

esp_err_t config_next_boot(uint32_t *boot)
{
    nvs_handle_t h;
    ESP_RETURN_ON_ERROR(nvs_open("spincycle", NVS_READWRITE, &h), TAG, "open");
    uint32_t n = 0;
    esp_err_t err = nvs_get_u32(h, "boot", &n);
    if (err == ESP_OK || err == ESP_ERR_NVS_NOT_FOUND) {
        err = nvs_set_u32(h, "boot", n + 1);
    }
    if (err == ESP_OK) {
        err = nvs_commit(h);
    }
    nvs_close(h);
    if (err == ESP_OK) {
        *boot = n + 1;
    }
    return err;
}
