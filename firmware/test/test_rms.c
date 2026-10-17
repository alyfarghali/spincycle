#include <assert.h>
#include <math.h>
#include <stdio.h>
#include "rms.h"

#define N 500
#define A 0.05f

static float x[N], y[N], z[N];

static void fill(float gx, float gy, float gz, float amp) {
    for (int i = 0; i < N; i++) {
        float s = amp * sinf(2.0f * (float)M_PI * 10.0f * i / 100.0f);
        x[i] = gx;
        y[i] = gy;
        z[i] = gz + s;
    }
}

static void check(const char *name, float expected) {
    float result = rms_window(x, y, z, N);
    printf("%-22s got %.5f want %.5f\n", name, result, expected);
    assert(fabsf(result - expected) < 1e-4f);
}

int main(void) {
    fill(0, 0, 1, 0);
    check("still, flat", 0);

    fill(0.6f, 0, 0.8f, 0);
    check("still, tilted", 0);

    fill(0, 0, 1, A);
    check("sine amplitude A", A / sqrtf(2));

    puts("ok");
}
