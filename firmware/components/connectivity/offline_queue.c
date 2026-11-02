#include "offline_queue.h"

void offline_queue_init(offline_queue_t *q)
{
    q->head = 0;
    q->count = 0;
}

bool offline_queue_push(offline_queue_t *q, const window_t *w)
{
    if (!w->ts_valid) {
        return false;
    }
    if (q->count == OFFLINE_QUEUE_CAP) {
        offline_queue_pop(q);
    }
    q->items[(q->head + q->count) % OFFLINE_QUEUE_CAP] = *w;
    q->count++;
    return true;
}

bool offline_queue_peek(const offline_queue_t *q, window_t *out)
{
    if (q->count == 0) {
        return false;
    }
    *out = q->items[q->head];
    return true;
}

void offline_queue_pop(offline_queue_t *q)
{
    if (q->count == 0) {
        return;
    }
    q->head = (q->head + 1) % OFFLINE_QUEUE_CAP;
    q->count--;
}
