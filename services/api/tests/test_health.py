import asyncio

from app.main import app
from app.routes import health
from app.store import postgres
from tests.conftest import running_api


async def test_ok_when_both_respond(client):
    resp = await client.get("/health")
    assert resp.status_code == 200
    assert resp.json() == {"status": "ok", "postgres": "ok", "redis": "ok"}


async def test_redis_down_is_503_and_postgres_still_reported(test_env, monkeypatch):
    monkeypatch.setenv("REDIS_URL", "redis://localhost:1/0")
    async with running_api() as client:
        resp = await client.get("/health")
    assert resp.status_code == 503
    assert resp.json() == {"status": "down", "postgres": "ok", "redis": "down"}


async def test_postgres_down_is_503_and_redis_still_reported(client):
    await app.state.pg.close()
    resp = await client.get("/health")
    assert resp.status_code == 503
    assert resp.json() == {"status": "down", "postgres": "down", "redis": "ok"}


async def test_hung_database_reads_as_down(client, monkeypatch):
    async def hangs(pool):
        await asyncio.sleep(60)

    monkeypatch.setattr(postgres, "ping", hangs)
    monkeypatch.setattr(health, "CHECK_TIMEOUT_S", 0.01)
    resp = await client.get("/health")
    assert resp.status_code == 503
    assert resp.json()["postgres"] == "down"
