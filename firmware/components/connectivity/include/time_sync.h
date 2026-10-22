#pragma once

#include <stdbool.h>
#include <sys/time.h>

void time_sync_start(void);
// False until SNTP has synced; callers send ts as null until then.
bool time_sync_now(struct timeval *out);
