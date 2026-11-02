#include <time.h>
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"
#include "unity.h"
#include "config.h"
#include "nvs.h"
#include "secrets.h"
#include "time_sync.h"
#include "wifi_manager.h"
#include "suites.h"

static void test_wifi_connects(void)
{
    TEST_ASSERT_EQUAL(ESP_OK, wifi_manager_start(DEV_WIFI_SSID, DEV_WIFI_PASSWORD));
    TEST_ASSERT_TRUE_MESSAGE(wifi_manager_wait_connected(30000),
                             "no IP in 30 s: check secrets.h and that the network is 2.4 GHz");
    TEST_ASSERT_TRUE(wifi_manager_wait_connected(0)); // stays set for every later waiter
}

static void test_clock_syncs_after_wifi(void)
{
    struct timeval tv;
    TEST_ASSERT_FALSE_MESSAGE(time_sync_now(&tv), "clock already valid before SNTP: test needs a fresh boot");
    time_sync_start();
    bool synced = false;
    for (int i = 0; i < 150 && !synced; i++) {
        vTaskDelay(pdMS_TO_TICKS(100));
        synced = time_sync_now(&tv);
    }
    TEST_ASSERT_TRUE_MESSAGE(synced, "no SNTP sync in 15 s: is UDP 123 blocked on this network?");
    struct tm t;
    gmtime_r(&tv.tv_sec, &t);
    TEST_ASSERT_GREATER_OR_EQUAL(2026, t.tm_year + 1900);
}

static void test_boot_counter_is_committed_and_increments(void)
{
    uint32_t first, second, stored;
    TEST_ASSERT_EQUAL(ESP_OK, config_next_boot(&first));
    TEST_ASSERT_GREATER_OR_EQUAL(1, first);
    TEST_ASSERT_EQUAL(ESP_OK, config_next_boot(&second));
    TEST_ASSERT_EQUAL_UINT32(first + 1, second);

    nvs_handle_t h;
    TEST_ASSERT_EQUAL(ESP_OK, nvs_open("spincycle", NVS_READONLY, &h));
    TEST_ASSERT_EQUAL(ESP_OK, nvs_get_u32(h, "boot", &stored));
    nvs_close(h);
    TEST_ASSERT_EQUAL_UINT32(second, stored);
}

void run_connectivity_tests(void)
{
    RUN_TEST(test_boot_counter_is_committed_and_increments);
    RUN_TEST(test_wifi_connects);
    RUN_TEST(test_clock_syncs_after_wifi);
}
