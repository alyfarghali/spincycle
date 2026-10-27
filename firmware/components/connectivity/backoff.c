#include "backoff.h"

#define BACKOFF_MAX_MS 60000

uint32_t backoff_ms(uint32_t attempt, float r)
{
    // Cap before shifting: a shift of 32 or more is undefined in C.
    uint32_t base = attempt < 6 ? 1000u << attempt : BACKOFF_MAX_MS;
    return (uint32_t)(base * (0.8f + 0.4f * r));
}
