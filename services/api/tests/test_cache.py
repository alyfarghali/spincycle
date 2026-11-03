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


async def test_evicts_least_recently_used():
    load = Loader()
    cache = TTLCache(max_entries=2, clock=Clock())
    await cache.get("a", 60, load)
    await cache.get("b", 60, load)
    await cache.get("a", 60, load)
    await cache.get("c", 60, load)
    assert await cache.get("a", 60, load) == 1
    assert await cache.get("b", 60, load) == 4


async def test_memory_stays_bounded_under_unique_keys():
    async def fails():
        raise OSError("redis down")

    cache = TTLCache(max_entries=100, clock=Clock())
    await asyncio.gather(*(cache.get(f"k{i}", 60, Loader()) for i in range(10000)))
    for i in range(100):
        with pytest.raises(OSError):
            await cache.get(f"bad{i}", 60, fails)
    assert len(cache._entries) == 100
    assert cache._loading == {}


async def test_concurrent_failure_shares_one_load():
    calls = []

    async def fails():
        calls.append(1)
        await asyncio.sleep(0)
        raise OSError("redis down")

    cache = TTLCache(clock=Clock())
    results = await asyncio.gather(*(cache.get("k", 2, fails) for _ in range(50)), return_exceptions=True)
    assert len(calls) == 1
    assert all(isinstance(r, OSError) for r in results)


async def test_cancelled_request_does_not_cancel_shared_load():
    release = asyncio.Event()

    async def slow():
        await release.wait()
        return "value"

    cache = TTLCache(clock=Clock())
    first = asyncio.create_task(cache.get("k", 2, slow))
    second = asyncio.create_task(cache.get("k", 2, slow))
    await asyncio.sleep(0)
    first.cancel()
    release.set()
    assert await second == "value"
