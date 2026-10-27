#include <math.h>
#include <sys/time.h>
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/queue.h"
#include "freertos/task.h"
#include "sdkconfig.h"
#include "unity.h"
#include "accel.h"
#include "sensor_task.h"
#include "suites.h"

#define ODR_HZ ((float)ACCEL_ODR_NUM / ACCEL_ODR_DEN)

static void test_accel_init_finds_icm20948(void)
{
    TEST_ASSERT_EQUAL(ESP_OK, accel_init(CONFIG_SPINCYCLE_I2C_SDA_GPIO, CONFIG_SPINCYCLE_I2C_SCL_GPIO));
}

static void test_still_board_reads_one_g(void)
{
    float sum = 0;
    for (int i = 0; i < 20; i++) {
        accel_sample_t s;
        TEST_ASSERT_EQUAL(ESP_OK, accel_read(&s));
        sum += sqrtf(s.x * s.x + s.y * s.y + s.z * s.z);
        vTaskDelay(pdMS_TO_TICKS(50));
    }
    TEST_ASSERT_FLOAT_WITHIN(0.05f, 1.0f, sum / 20);
}

static void test_fifo_delivers_102_hz(void)
{
    accel_sample_t buf[64];
    size_t total = 0, got;
    TEST_ASSERT_EQUAL(ESP_OK, accel_fifo_reset());
    int64_t start = esp_timer_get_time();
    for (int i = 0; i < 16; i++) {
        vTaskDelay(pdMS_TO_TICKS(250));
        TEST_ASSERT_EQUAL(ESP_OK, accel_fifo_read(buf, 64, &got));
        total += got;
    }
    float secs = (esp_timer_get_time() - start) / 1e6f;
    TEST_ASSERT_FLOAT_WITHIN(0.03f * ODR_HZ, ODR_HZ, total / secs);
}

static void test_fifo_overflow_is_reported_then_recovers(void)
{
    accel_sample_t buf[64];
    size_t got;
    TEST_ASSERT_EQUAL(ESP_OK, accel_fifo_reset());
    vTaskDelay(pdMS_TO_TICKS(1500)); // 512-byte FIFO fills in ~0.8 s at 6 bytes per sample
    TEST_ASSERT_EQUAL(ESP_ERR_INVALID_STATE, accel_fifo_read(buf, 64, &got));
    TEST_ASSERT_EQUAL(0, got);

    TEST_ASSERT_EQUAL(ESP_OK, accel_fifo_reset());
    vTaskDelay(pdMS_TO_TICKS(250));
    TEST_ASSERT_EQUAL(ESP_OK, accel_fifo_read(buf, 64, &got));
    TEST_ASSERT_GREATER_THAN(0, got);
}

void run_accel_tests(void)
{
    RUN_TEST(test_accel_init_finds_icm20948);
    RUN_TEST(test_still_board_reads_one_g);
    RUN_TEST(test_fifo_delivers_102_hz);
    RUN_TEST(test_fifo_overflow_is_reported_then_recovers);
}

static void test_sensor_task_rejects_bad_arguments(void)
{
    QueueHandle_t q = xQueueCreate(4, sizeof(window_t));
    TEST_ASSERT_EQUAL(ESP_ERR_INVALID_ARG, sensor_task_start(NULL, 5));
    TEST_ASSERT_EQUAL(ESP_ERR_INVALID_ARG, sensor_task_start(q, 0));
    TEST_ASSERT_EQUAL(ESP_ERR_INVALID_ARG, sensor_task_start(q, 61));
    vQueueDelete(q);
}

static void test_sensor_task_windows(void)
{
    QueueHandle_t q = xQueueCreate(4, sizeof(window_t));
    TEST_ASSERT_EQUAL(ESP_OK, sensor_task_start(q, 5));

    window_t w[3];
    int64_t arrived[3];
    for (int i = 0; i < 3; i++) {
        TEST_ASSERT_EQUAL(pdTRUE, xQueueReceive(q, &w[i], pdMS_TO_TICKS(12000)));
        arrived[i] = esp_timer_get_time();
    }
    for (int i = 0; i < 3; i++) {
        TEST_ASSERT_EQUAL_UINT64(i, w[i].seq);
        TEST_ASSERT_TRUE(isfinite(w[i].rms));
        TEST_ASSERT_GREATER_THAN_FLOAT(0.0002f, w[i].rms); // a real sensor always has some noise
        TEST_ASSERT_LESS_THAN_FLOAT(0.005f, w[i].rms);     // board must be still: bench floor is ~0.0015 g
        TEST_ASSERT_TRUE(w[i].ts_valid);                   // the clock synced earlier in this run
    }
    for (int i = 1; i < 3; i++) {
        float gap = (arrived[i] - arrived[i - 1]) / 1e6f;
        TEST_ASSERT_FLOAT_WITHIN(0.3f, 5.0f, gap); // 511 samples at 102.3 Hz
    }
    struct timeval now;
    gettimeofday(&now, NULL);
    TEST_ASSERT_INT_WITHIN(2, now.tv_sec, w[2].ts.tv_sec);
}

void run_sensor_task_tests(void)
{
    RUN_TEST(test_sensor_task_rejects_bad_arguments);
    RUN_TEST(test_sensor_task_windows);
}
