#include <rms.h>
#include <stddef.h>
#include <math.h>

float static magnitude(float x, float y, float z) {
    return sqrtf(x * x + y * y + z * z);
}

float rms_window(const float *x, const float *y, const float *z, size_t n) {
    float mean = 0.0f;
    float sum_sq = 0.0f;

    for (size_t i = 0; i < n; i++) {
        mean += magnitude(x[i], y[i], z[i]);
    }
    mean = mean / n;

    for (size_t i = 0; i < n; i++) {
        float deviation = magnitude(x[i], y[i], z[i]) - mean;
        sum_sq += deviation * deviation;
    }

    return sqrtf(sum_sq / n);
}
