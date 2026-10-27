#include <assert.h>
#include <math.h>
#include <stdio.h>
#include <stdlib.h>
#include "rms.h"

#define N 500
#define A 0.05f

static float x[N], y[N], z[N];

static void fill(float gx, float gy, float gz, float amp, int axis) {
    for (int i = 0; i < N; i++) {
        float s = amp * sinf(2.0f * (float)M_PI * 10.0f * i / 100.0f);
        x[i] = gx + (axis == 0 ? s : 0);
        y[i] = gy + (axis == 1 ? s : 0);
        z[i] = gz + (axis == 2 ? s : 0);
    }
}

static void check(const char *name, float expected, float tolerance) {
    float result = rms_window(x, y, z, N);
    printf("%-38s got %.5f want %.5f\n", name, result, expected);
    assert(fabsf(result - expected) <= tolerance);
}

int main(void) {
    fill(0, 0, 1, 0, 2);
    check("still, flat", 0, 1e-4f);
    fill(0.6f, 0, 0.8f, 0, 2);
    check("still, tilted", 0, 1e-4f);
    fill(0, 1, 0, 0, 2);
    check("still, on its side", 0, 1e-4f);
    fill(0, 0, 1.03f, 0, 2);
    check("still, sensor reads 3 % high", 0, 1e-4f);

    // Along gravity the magnitude is 1 + A sin, so RMS = A / √2.
    fill(0, 0, 1, A, 2);
    check("sine amplitude A along gravity (z)", A / sqrtf(2), 1e-4f);
    fill(1, 0, 0, A, 0);
    check("same, mounted sideways (x)", A / sqrtf(2), 1e-4f);
    fill(0, 0, 1, 2 * A, 2);
    check("double amplitude doubles RMS", 2 * A / sqrtf(2), 2e-4f);

    // The real ODR is 102.3 Hz, so a window doesn't hold whole periods of the vibration.
    for (int i = 0; i < N; i++) {
        x[i] = 0;
        y[i] = 0;
        z[i] = 1 + A * sinf(2.0f * (float)M_PI * 10.0f * i / (1125.0f / 11.0f));
    }
    check("sine, non-whole periods", A / sqrtf(2), 0.02f * A);

    // Gaussian noise of σ (Box–Muller): the still-machine noise floor.
    srand(1);
    float sigma = 0.0015f;
    for (int i = 0; i < N; i++) {
        float u1 = (rand() + 1.0f) / (RAND_MAX + 2.0f), u2 = (rand() + 1.0f) / (RAND_MAX + 2.0f);
        x[i] = 0;
        y[i] = 0;
        z[i] = 1 + sigma * sqrtf(-2 * logf(u1)) * cosf(2 * (float)M_PI * u2);
    }
    check("gaussian noise sigma", sigma, 0.15f * sigma);

    // sensor_task never passes n = 0; if it did, NaN is rejected by telemetry_build instead of sent.
    assert(isnan(rms_window(x, y, z, 0)));

    puts("ok");
}
