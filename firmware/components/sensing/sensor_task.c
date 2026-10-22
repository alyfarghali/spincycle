// FreeRTOS task: collect one 5 s window of samples and push its RMS onto the publish queue.
// The accelerometer's FIFO paces sampling; this task only drains it and does the window math.
#include "sensor_task.h"

#include <sys/time.h>
#include "esp_log.h"
#include "freertos/task.h"
#include "accel.h"
#include "rms.h"

#define MAX_INTERVAL_S  60
#define MAX_SAMPLES     (MAX_INTERVAL_S * ACCEL_ODR_NUM / ACCEL_ODR_DEN + 1)
#define DRAIN_MS        250  // ~25 frames per drain; the FIFO overflows after ~800 ms
#define DRAIN_BATCH     64

// Before SNTP syncs the clock reads 1970. Checking the year here keeps sensing
// independent of the connectivity component (which already depends on sensing).
#define CLOCK_VALID_AFTER 1735689600  // 2025-01-01T00:00:00Z

static const char *TAG = "sensor";

// static, not on the task stack: 3 x ~6100 floats = ~72 KB.
static float s_x[MAX_SAMPLES], s_y[MAX_SAMPLES], s_z[MAX_SAMPLES];

static QueueHandle_t s_out;
static uint16_t s_interval_s;

// Fills s_x/s_y/s_z with `expected` consecutive samples.
// Returns false if that doesn't happen within 2x the window length (sensor stuck or I2C failing).
static bool collect_window(size_t expected)
{
    accel_sample_t buf[DRAIN_BATCH];
    size_t n = 0;
    TickType_t deadline = xTaskGetTickCount() + pdMS_TO_TICKS(2000u * s_interval_s);

    while (n < expected) {
        if ((int32_t)(xTaskGetTickCount() - deadline) > 0) {
            return false;
        }
        vTaskDelay(pdMS_TO_TICKS(DRAIN_MS));  // sets drain rate only; sample timing comes from the sensor

        size_t got = 0;
        esp_err_t err = accel_fifo_read(buf, DRAIN_BATCH, &got);
        if (err == ESP_ERR_INVALID_STATE) {
            // Samples were lost, so the window no longer covers continuous time: start it over.
            ESP_LOGW(TAG, "fifo overflow, restarting window");
            accel_fifo_reset();
            n = 0;
            continue;
        }
        if (err != ESP_OK) {
            continue;  // transient I2C error: the samples are still in the FIFO for the next drain
        }

        for (size_t i = 0; i < got && n < expected; i++) {
            s_x[n] = buf[i].x;
            s_y[n] = buf[i].y;
            s_z[n] = buf[i].z;
            n++;
        }
        // Samples read past `expected` (at most one drain's worth) are dropped; the next
        // window starts from whatever is still in the FIFO.
    }
    return true;
}

static void sensor_task(void *arg)
{
    uint64_t seq = 0;
    accel_fifo_reset();  // discard anything queued before the task started

    for (;;) {
        // Sample count defines the window: 5 s x 102.3 Hz = 511 samples ~= 5.0 s.
        size_t expected = (size_t)s_interval_s * ACCEL_ODR_NUM / ACCEL_ODR_DEN;

        window_t w = { .seq = seq++ };  // a skipped window still uses its seq, so the gap shows server-side

        if (!collect_window(expected)) {
            ESP_LOGW(TAG, "window %llu skipped: sensor not delivering samples", w.seq);
            accel_fifo_reset();
            continue;
        }

        w.rms = rms_window(s_x, s_y, s_z, expected);
        gettimeofday(&w.ts, NULL);  // window end time
        w.ts_valid = w.ts.tv_sec > CLOCK_VALID_AFTER;

        // Never block: a stalled publisher must not stop sampling.
        if (xQueueSend(s_out, &w, 0) != pdTRUE) {
            ESP_LOGW(TAG, "window %llu dropped: queue full", w.seq);
        }
    }
}

esp_err_t sensor_task_start(QueueHandle_t out, uint16_t interval_s)
{
    if (out == NULL || interval_s == 0 || interval_s > MAX_INTERVAL_S) {
        return ESP_ERR_INVALID_ARG;
    }
    s_out = out;
    s_interval_s = interval_s;

    // Stack: buf[64] samples = 768 B plus logging; 4 KB is comfortable.
    BaseType_t ok = xTaskCreate(sensor_task, "sensor", 4096, NULL, 5, NULL);
    return ok == pdPASS ? ESP_OK : ESP_ERR_NO_MEM;
}
