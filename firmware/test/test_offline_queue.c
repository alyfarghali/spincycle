#include <assert.h>
#include <stdio.h>
#include "offline_queue.h"

static window_t win(uint64_t seq)
{
    return (window_t){ .seq = seq, .ts_valid = true };
}

int main(void)
{
    offline_queue_t q;
    window_t w;
    offline_queue_init(&q);

    assert(!offline_queue_peek(&q, &w));
    offline_queue_pop(&q); // empty pop must not underflow the count
    assert(!offline_queue_peek(&q, &w));

    // Contract A5: readings without a ts are never queued.
    window_t no_ts = { .seq = 7, .ts_valid = false };
    assert(!offline_queue_push(&q, &no_ts));
    assert(!offline_queue_peek(&q, &w));

    // Oldest first; peek doesn't remove, so a failed publish keeps the reading.
    assert(offline_queue_push(&q, &(window_t){ .seq = 1, .ts_valid = true }));
    w = win(2);
    assert(offline_queue_push(&q, &w));
    assert(offline_queue_peek(&q, &w) && w.seq == 1);
    assert(offline_queue_peek(&q, &w) && w.seq == 1);
    offline_queue_pop(&q);
    assert(offline_queue_peek(&q, &w) && w.seq == 2);
    offline_queue_pop(&q);
    assert(!offline_queue_peek(&q, &w));

    // Contract A5: at least 5 minutes at 5 s per reading.
    assert(OFFLINE_QUEUE_CAP >= 60);

    // Full: drops the oldest, keeps the newest CAP, across the ring's wrap-around.
    const uint64_t pushed = OFFLINE_QUEUE_CAP + 10;
    for (uint64_t s = 0; s < pushed; s++) {
        w = win(s);
        assert(offline_queue_push(&q, &w));
    }
    for (uint64_t s = pushed - OFFLINE_QUEUE_CAP; s < pushed; s++) {
        assert(offline_queue_peek(&q, &w) && w.seq == s);
        offline_queue_pop(&q);
    }
    assert(!offline_queue_peek(&q, &w));

    puts("ok");
    return 0;
}
