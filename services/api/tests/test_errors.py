import asyncpg
import httpx
import pytest
from fastapi import FastAPI
from redis.exceptions import ConnectionError as RedisConnectionError

from app.main import app
from app.routes import errors


def client_for(a):
    return httpx.AsyncClient(transport=httpx.ASGITransport(app=a), base_url="http://test")


def app_raising(exc):
    a = FastAPI()
    errors.install(a)

    @a.get("/boom")
    async def boom():
        raise exc

    return a


@pytest.mark.parametrize(
    ("exc", "status", "error", "message"),
    [
        (errors.fleet_not_found("laundy"), 404, "fleet_not_found", "No fleet with ID 'laundy'."),
        (errors.machine_not_found("x"), 404, "machine_not_found", "No machine with ID 'x'."),
    ],
)
async def test_api_errors_have_the_contract_shape(exc, status, error, message):
    async with client_for(app_raising(exc)) as c:
        resp = await c.get("/boom")
    assert resp.status_code == status
    assert resp.json() == {"error": error, "message": message}


@pytest.mark.parametrize(
    "exc",
    [
        ConnectionRefusedError("refused"),
        TimeoutError("slow"),
        asyncpg.PostgresConnectionError("gone"),
        asyncpg.InterfaceError("pool closed"),
        asyncpg.CannotConnectNowError("starting up"),
        asyncpg.AdminShutdownError("shutting down"),
        asyncpg.TooManyConnectionsError("full"),
        RedisConnectionError("gone"),
    ],
)
async def test_unreachable_database_is_503(exc):
    async with client_for(app_raising(exc)) as c:
        resp = await c.get("/boom")
    assert resp.status_code == 503
    assert resp.json()["error"] == "unavailable"
    assert str(exc) not in resp.json()["message"]


@pytest.mark.parametrize("query", ["limit=0", "limit=501", "limit=abc", "state=bogus"])
async def test_bad_query_is_400_invalid_query(query):
    async with client_for(app) as c:
        resp = await c.get(f"/api/fleets/laundry/machines?{query}")
    assert resp.status_code == 400
    body = resp.json()
    assert body["error"] == "invalid_query"
    assert set(body) == {"error", "message"}
