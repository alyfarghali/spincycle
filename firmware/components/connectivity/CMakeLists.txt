idf_component_register(SRCS "telemetry.c" "wifi_manager.c" "time_sync.c" "backoff.c" "offline_queue.c" "mqtt_task.c"
                       INCLUDE_DIRS "include"
                       REQUIRES sensing device_config
                       PRIV_REQUIRES esp_wifi esp_netif esp_event esp_timer mbedtls espressif__mqtt)
