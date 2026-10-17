#pragma once

#include <stdbool.h>
#include <stddef.h>
#include "window.h"

#define OFFLINE_QUEUE_CAP 60

typedef struct {
    window_t items[OFFLINE_QUEUE_CAP];
    size_t head, count;
} offline_queue_t;

void offline_queue_init(offline_queue_t *q);
// Drops the oldest when full. Returns false for a window without a valid ts (contract A5).
bool offline_queue_push(offline_queue_t *q, const window_t *w);
// Peek, publish, then pop: a failed publish leaves the reading queued.
bool offline_queue_peek(const offline_queue_t *q, window_t *out);
void offline_queue_pop(offline_queue_t *q);
