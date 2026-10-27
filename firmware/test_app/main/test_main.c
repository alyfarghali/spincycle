#include "esp_event.h"
#include "esp_log.h"
#include "esp_netif.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"
#include "nvs_flash.h"
#include "unity.h"
#include "suites.h"

void app_main(void)
{
    // Gives the host time to reopen the serial port after flashing, so no result is missed.
    vTaskDelay(pdMS_TO_TICKS(3000));
    esp_log_level_set("wifi", ESP_LOG_WARN);
    ESP_ERROR_CHECK(nvs_flash_init());
    ESP_ERROR_CHECK(esp_netif_init());
    ESP_ERROR_CHECK(esp_event_loop_create_default());

    // Order matters: accel_init and sensor_task_start can each run once, and the sensor task
    // test checks timestamps, so it needs Wi-Fi and the clock first.
    UNITY_BEGIN();
    run_target_tests();
    run_accel_tests();
    run_connectivity_tests();
    run_sensor_task_tests();
    UNITY_END();
}
