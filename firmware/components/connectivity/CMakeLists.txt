idf_component_register(SRCS "telemetry.c" "wifi_manager.c" "time_sync.c"
                       INCLUDE_DIRS "include"
                       PRIV_REQUIRES esp_wifi esp_netif esp_event esp_timer)
