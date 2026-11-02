import asyncio

import pytest

from app.cache import TTLCache


class Loader:
    def __init__(self):
        self.calls = 0

    async def __call__(self):
        self.calls += 1
        # Yields to the event loop, so concurrent callers really do overlap with this load.
        await asyncio.sleep(0)
        return self.calls


class Clock:
    def __init__(self):
        self.now = 0.0

    def __call__(self):
        return self.now


async def test_within_ttl_does_not_load_again():
    clock, load = Clock(), Loader()
    cache = TTLCache(clock=clock)
    assert await cache.get("k", 2, load) == 1
    clock.now = 1.9
    assert await cache.get("k", 2, load) == 1
    assert load.calls == 1


async def test_after_ttl_loads_again():
    clock, load = Clock(), Loader()
    cache = TTLCache(clock=clock)
    await cache.get("k", 2, load)
    clock.now = 2.0
    assert await cache.get("k", 2, load) == 2
    assert load.calls == 2


async def test_concurrent_requests_share_one_load():
    load = Loader()
    cache = TTLCache(clock=Clock())
    results = await asyncio.gather(*(cache.get("k", 2, load) for _ in range(50)))
    assert load.calls == 1
    assert results == [1] * 50


async def test_concurrent_requests_after_expiry_share_one_load():
    clock, load = Clock(), Loader()
    cache = TTLCache(clock=clock)
    await cache.get("k", 2, load)
    clock.now = 5.0
    results = await asyncio.gather(*(cache.get("k", 2, load) for _ in range(50)))
    assert load.calls == 2
    assert results == [2] * 50


async def test_failure_is_not_cached():
    async def fails():
        raise OSError("redis down")

    load = Loader()
    cache = TTLCache(clock=Clock())
    with pytest.raises(OSError):
        await cache.get("k", 2, fails)
    assert await cache.get("k", 2, load) == 1


async def test_none_is_cached():
    calls = []

    async def missing():
        calls.append(1)

    cache = TTLCache(clock=Clock())
    assert await cache.get("k", 60, missing) is None
    assert await cache.get("k", 60, missing) is None
    assert len(calls) == 1


async def test_keys_are_separate():
    load = Loader()
    cache = TTLCache(clock=Clock())
    assert await cache.get("a", 2, load) == 1
    assert await cache.get("b", 2, load) == 2
    assert await cache.get("a", 2, load) == 1
