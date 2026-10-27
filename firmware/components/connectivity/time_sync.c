#include "time_sync.h"

#include "esp_log.h"
#include "esp_netif_sntp.h"

// The clock counts from 1970 until the first sync, so anything before 2025 means "not synced".
#define CLOCK_VALID_AFTER 1735689600  // 2025-01-01T00:00:00Z

static const char *TAG = "time_sync";

static void on_sync(struct timeval *tv)
{
    ESP_LOGI(TAG, "clock synced");
}

void time_sync_start(void)
{
    esp_sntp_config_t cfg = ESP_NETIF_SNTP_DEFAULT_CONFIG("pool.ntp.org");
    cfg.sync_cb = on_sync;
    esp_err_t err = esp_netif_sntp_init(&cfg);
    if (err != ESP_OK) {
        ESP_LOGW(TAG, "SNTP start failed: %s", esp_err_to_name(err));
    }
}

bool time_sync_now(struct timeval *out)
{
    gettimeofday(out, NULL);
    return out->tv_sec > CLOCK_VALID_AFTER;
}
