#include <assert.h>
#include <stdint.h>
#include <stdio.h>
#include "backoff.h"

int main(void)
{
    // Contract A2: 1, 2, 4, 8 ... capped at 60 s.
    const uint32_t base[] = {1000, 2000, 4000, 8000, 16000, 32000, 60000, 60000};
    for (uint32_t a = 0; a < sizeof base / sizeof base[0]; a++) {
        assert(backoff_ms(a, 0.5f) == base[a]);
    }

    // Never overflows or wraps, however long the outage.
    const uint32_t huge[] = {7, 31, 32, 33, 64, 1000, UINT32_MAX};
    for (int i = 0; i < 7; i++) {
        assert(backoff_ms(huge[i], 0.5f) == 60000);
    }

    // ±20 % jitter: r = 0 → 0.8 × base, r just under 1 → just under 1.2 × base.
    for (uint32_t a = 0; a < 8; a++) {
        uint32_t lo = backoff_ms(a, 0.0f), hi = backoff_ms(a, 0.99999f);
        assert(lo == base[a] * 8 / 10);
        assert(hi < base[a] * 12 / 10 && hi > base[a] * 119 / 100);
        for (float r = 0; r < 1; r += 0.01f) {
            uint32_t d = backoff_ms(a, r);
            assert(d >= lo && d <= hi);
        }
    }

    // Jitter actually spreads devices out: different r, different delay.
    assert(backoff_ms(3, 0.1f) != backoff_ms(3, 0.9f));

    puts("ok");
}
