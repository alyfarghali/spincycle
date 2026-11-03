import os
from contextlib import asynccontextmanager

import httpx
import pytest
import redis.asyncio

from app.main import app


@asynccontextmanager
async def running_api():
    async with app.router.lifespan_context(app):
        transport = httpx.ASGITransport(app=app)
        async with httpx.AsyncClient(transport=transport, base_url="http://test") as client:
            yield client


@pytest.fixture
async def test_env(monkeypatch):
    database_url = os.environ.get("TEST_DATABASE_URL")
    redis_url = os.environ.get("TEST_REDIS_URL")
    if not database_url or not redis_url:
        pytest.skip("TEST_DATABASE_URL and TEST_REDIS_URL not set")
    # The next line wipes the database, so never let it reach live data.
    assert redis_url.endswith("/15"), "TEST_REDIS_URL must use Redis database 15"
    r = redis.asyncio.Redis.from_url(redis_url)
    await r.flushdb()
    await r.aclose()
    monkeypatch.setenv("DATABASE_URL", database_url)
    monkeypatch.setenv("REDIS_URL", redis_url)


@pytest.fixture
async def client(test_env):
    async with running_api() as c:
        yield c
