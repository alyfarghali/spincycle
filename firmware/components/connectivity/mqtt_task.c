#include "mqtt_task.h"
#include "backoff.h"
#include "offline_queue.h"
#include "telemetry.h"

#include <stdatomic.h>
#include <stdio.h>
#include "esp_check.h"
#include "esp_crt_bundle.h"
#include "esp_log.h"
#include "esp_random.h"
#include "esp_timer.h"
#include "freertos/task.h"
#include "mqtt_client.h"

#define FW_VERSION "0.1.0"
#define TOPIC_LEN  96

static const char *TAG = "mqtt";

static device_config_t s_cfg;
static uint32_t s_boot;
static QueueHandle_t s_windows;
static esp_mqtt_client_handle_t s_client;
static esp_timer_handle_t s_retry_timer;
static uint32_t s_attempt;
static atomic_bool s_connected;
static offline_queue_t s_offline;
static char s_telemetry_topic[TOPIC_LEN], s_status_topic[TOPIC_LEN], s_config_topic[TOPIC_LEN];

static void retry_cb(void *arg)
{
    esp_mqtt_client_reconnect(s_client);
}

static void on_event(void *arg, esp_event_base_t base, int32_t id, void *data)
{
    esp_mqtt_event_handle_t e = data;
    switch ((esp_mqtt_event_id_t)id) {
    case MQTT_EVENT_CONNECTED:
        ESP_LOGI(TAG, "connected");
        s_attempt = 0;
        // Contract A2 order: online, then subscribe, and only then may telemetry flow.
        esp_mqtt_client_publish(s_client, s_status_topic, "online", 0, 1, 1);
        esp_mqtt_client_subscribe(s_client, s_config_topic, 1);
        atomic_store(&s_connected, true);
        break;
    case MQTT_EVENT_DISCONNECTED: {
        atomic_store(&s_connected, false);
        uint32_t delay = backoff_ms(s_attempt++, esp_random() / 4294967296.0f);
        ESP_LOGW(TAG, "disconnected, retrying in %lu ms", (unsigned long)delay);
        esp_timer_start_once(s_retry_timer, delay * 1000ULL);
        break;
    }
    case MQTT_EVENT_ERROR:
        if (e->error_handle->error_type == MQTT_ERROR_TYPE_CONNECTION_REFUSED) {
            ESP_LOGW(TAG, "broker refused connection, code %d (4 or 5 = wrong credentials)",
                     e->error_handle->connect_return_code);
        } else {
            ESP_LOGW(TAG, "connection error: %s", esp_err_to_name(e->error_handle->esp_tls_last_esp_err));
        }
        break;
    case MQTT_EVENT_DATA:
        ESP_LOGI(TAG, "config received, not applied: %.*s", e->data_len, e->data);
        break;
    default:
        break;
    }
}

// False means try again later. A reading that can't be encoded (non-finite RMS) never will be, so it counts as done.
static bool publish_window(const window_t *w)
{
    char payload[256];
    int n = telemetry_build(payload, sizeof payload, s_cfg.device_id, FW_VERSION, s_boot, w->seq,
                            w->ts_valid ? &w->ts : NULL, w->rms);
    if (n < 0) {
        ESP_LOGW(TAG, "window %llu dropped: can't encode", w->seq);
        return true;
    }
    return esp_mqtt_client_publish(s_client, s_telemetry_topic, payload, n, 1, 0) >= 0;
}

static void flush_offline(void)
{
    window_t w;
    while (atomic_load(&s_connected) && offline_queue_peek(&s_offline, &w) && publish_window(&w)) {
        offline_queue_pop(&s_offline);
    }
}

static void mqtt_task(void *arg)
{
    for (;;) {
        window_t w;
        xQueueReceive(s_windows, &w, portMAX_DELAY);
        ESP_LOGI(TAG, "window seq=%llu rms=%.5f g ts_valid=%d", w.seq, w.rms, w.ts_valid);

        flush_offline();
        // Our queue, not esp-mqtt's outbox, while disconnected: the outbox expires messages after 30 s.
        if ((!atomic_load(&s_connected) || !publish_window(&w)) && !offline_queue_push(&s_offline, &w)) {
            ESP_LOGW(TAG, "window %llu dropped: disconnected with no valid ts", w.seq);
        }
    }
}

esp_err_t mqtt_task_start(const device_config_t *cfg, uint32_t boot, QueueHandle_t windows)
{
    s_cfg = *cfg;
    s_boot = boot;
    s_windows = windows;
    offline_queue_init(&s_offline);

    const char *fmt = "fleets/%s/devices/%s/%s";
    snprintf(s_telemetry_topic, TOPIC_LEN, fmt, s_cfg.fleet_id, s_cfg.device_id, "telemetry");
    snprintf(s_status_topic, TOPIC_LEN, fmt, s_cfg.fleet_id, s_cfg.device_id, "status");
    snprintf(s_config_topic, TOPIC_LEN, fmt, s_cfg.fleet_id, s_cfg.device_id, "config");

    const esp_timer_create_args_t timer_args = { .callback = retry_cb, .name = "mqtt_retry" };
    ESP_RETURN_ON_ERROR(esp_timer_create(&timer_args, &s_retry_timer), TAG, "retry timer");

    const esp_mqtt_client_config_t mqtt_cfg = {
        .broker.address.uri = s_cfg.broker_url,
        // Checks the chain and the hostname (contract A2).
        .broker.verification.crt_bundle_attach = esp_crt_bundle_attach,
        .credentials.client_id = s_cfg.device_id,
        .credentials.username = s_cfg.device_id,
        .credentials.authentication.password = s_cfg.mqtt_pass,
        .session.protocol_ver = MQTT_PROTOCOL_V_3_1_1,
        .session.keepalive = 30,
        .session.last_will = {
            .topic = s_status_topic,
            .msg = "offline",
            .qos = 1,
            .retain = 1,
        },
        // esp-mqtt's own retry uses a fixed delay; the contract needs backoff with jitter.
        .network.disable_auto_reconnect = true,
    };
    s_client = esp_mqtt_client_init(&mqtt_cfg);
    ESP_RETURN_ON_FALSE(s_client, ESP_ERR_NO_MEM, TAG, "client init");
    ESP_RETURN_ON_ERROR(esp_mqtt_client_register_event(s_client, ESP_EVENT_ANY_ID, on_event, NULL), TAG, "events");
    ESP_RETURN_ON_ERROR(esp_mqtt_client_start(s_client), TAG, "client start");

    BaseType_t ok = xTaskCreate(mqtt_task, "mqtt_pub", 4096, NULL, 5, NULL);
    return ok == pdPASS ? ESP_OK : ESP_ERR_NO_MEM;
}
