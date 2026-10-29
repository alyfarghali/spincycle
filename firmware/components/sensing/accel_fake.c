// Fake accelerometer for bench work without the ICM-20948 (CONFIG_SPINCYCLE_FAKE_ACCEL).
// Implements accel.h so sensor_task, rms_window and everything downstream run unchanged.
// Signal: gravity on z, small noise on every axis, and while "running" a 15 Hz shake on z.
// The cycle repeats: idle for FAKE_IDLE_S, running for FAKE_RUN_S.
#include "accel.h"

#include <math.h>
#include "esp_log.h"
#include "esp_random.h"
#include "esp_timer.h"
#include "sdkconfig.h"

#define FIFO_FRAMES   85      // 512-byte FIFO / 6 bytes per frame, same limit as the real chip
#define NOISE_G       0.002f  // uniform +-, gives an idle rms of ~0.001 g
#define SHAKE_G       0.07f   // peak, gives a running rms of ~0.05 g
#define SHAKE_HZ      15.0f   // ~900 rpm spin; well under the 51 Hz Nyquist limit

static const char *TAG = "accel_fake";

static int64_t s_fifo_t0_us;  // time of the last FIFO reset
static int64_t s_emitted;     // samples handed out since then

static float noise(void)
{
    return ((float)esp_random() / UINT32_MAX * 2.0f - 1.0f) * NOISE_G;
}

static bool running_at(int64_t t_us)
{
    int64_t period_s = CONFIG_SPINCYCLE_FAKE_IDLE_S + CONFIG_SPINCYCLE_FAKE_RUN_S;
    return (t_us / 1000000) % period_s >= CONFIG_SPINCYCLE_FAKE_IDLE_S;
}

static void sample_at(int64_t t_us, accel_sample_t *out)
{
    float shake = 0.0f;
    if (running_at(t_us)) {
        // Phase from the position within the current second: 15 Hz fits whole cycles in a second,
        // and a float of the full uptime in us would lose precision after a few hours.
        float in_second_s = (float)(t_us % 1000000) / 1e6f;
        shake = SHAKE_G * sinf(2.0f * (float)M_PI * SHAKE_HZ * in_second_s);
    }
    out->x = noise();
    out->y = noise();
    out->z = 1.0f + shake + noise();
}

// Time of sample number n after the reset, on the same 1125/11 Hz grid as the real chip.
static int64_t sample_time_us(int64_t n)
{
    return s_fifo_t0_us + n * ACCEL_ODR_DEN * 1000000 / ACCEL_ODR_NUM;
}

esp_err_t accel_init(int sda_gpio, int scl_gpio)
{
    (void)sda_gpio;
    (void)scl_gpio;
    ESP_LOGW(TAG, "FAKE ACCELEROMETER: synthetic data, idle %d s / running %d s. Never record or publish "
                  "this as a real device.",
             CONFIG_SPINCYCLE_FAKE_IDLE_S, CONFIG_SPINCYCLE_FAKE_RUN_S);
    return accel_fifo_reset();
}

esp_err_t accel_read(accel_sample_t *out)
{
    sample_at(esp_timer_get_time(), out);
    return ESP_OK;
}

esp_err_t accel_fifo_reset(void)
{
    s_fifo_t0_us = esp_timer_get_time();
    s_emitted = 0;
    return ESP_OK;
}

esp_err_t accel_fifo_read(accel_sample_t *out, size_t max, size_t *got)
{
    *got = 0;
    int64_t elapsed_us = esp_timer_get_time() - s_fifo_t0_us;
    int64_t due = elapsed_us * ACCEL_ODR_NUM / ((int64_t)ACCEL_ODR_DEN * 1000000) - s_emitted;

    // Drained too slowly: the real FIFO would have overwritten samples, so report it the same way.
    if (due > FIFO_FRAMES) {
        return ESP_ERR_INVALID_STATE;
    }

    size_t n = due < (int64_t)max ? (size_t)due : max;
    for (size_t i = 0; i < n; i++) {
        sample_at(sample_time_us(s_emitted + i), &out[i]);
    }
    s_emitted += n;
    *got = n;
    return ESP_OK;
}
