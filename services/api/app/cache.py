import asyncio
import time
from collections import OrderedDict
from collections.abc import Awaitable, Callable
from typing import Any


class TTLCache:
    def __init__(self, max_entries: int = 1000, clock: Callable[[], float] = time.monotonic) -> None:
        self._max_entries = max_entries
        self._clock = clock
        self._entries: OrderedDict[str, tuple[float, Any]] = OrderedDict()
        self._loading: dict[str, asyncio.Task] = {}

    async def get(self, key: str, ttl: float, load: Callable[[], Awaitable[Any]]) -> Any:
        stored_at, value = self._entries.get(key, (None, None))
        if stored_at is not None and self._clock() - stored_at < ttl:
            self._entries.move_to_end(key)
            return value

        task = self._loading.get(key)
        if task is None:
            task = self._loading[key] = asyncio.ensure_future(self._load(key, load))
        # Shielded so a client that disconnects doesn't cancel the load other requests are waiting on.
        return await asyncio.shield(task)

    async def _load(self, key: str, load: Callable[[], Awaitable[Any]]) -> Any:
        try:
            value = await load()
        finally:
            del self._loading[key]
        self._entries[key] = (self._clock(), value)
        self._entries.move_to_end(key)
        if len(self._entries) > self._max_entries:
            self._entries.popitem(last=False)
        return value
