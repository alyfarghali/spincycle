import asyncio
import time
from collections.abc import Awaitable, Callable
from typing import Any


class TTLCache:
    def __init__(self, clock: Callable[[], float] = time.monotonic) -> None:
        self._clock = clock
        self._entries: dict[str, tuple[float, Any]] = {}
        self._locks: dict[str, asyncio.Lock] = {}

    async def get(self, key: str, ttl: float, load: Callable[[], Awaitable[Any]]) -> Any:
        entry = self._entries.get(key)
        if entry is not None and self._clock() - entry[0] < ttl:
            return entry[1]

        lock = self._locks.setdefault(key, asyncio.Lock())
        async with lock:
            # Another request may have refreshed the entry while this one waited for the lock.
            entry = self._entries.get(key)
            if entry is not None and self._clock() - entry[0] < ttl:
                return entry[1]

            value = await load()
            self._entries[key] = (self._clock(), value)
            return value
