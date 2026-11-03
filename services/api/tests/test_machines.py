import os

import asyncpg
import pytest
import redis.asyncio

from app.cache import TTLCache
from app.main import app

FLEET = "apitest-m"
STATES = ["free", "running", "finished"]


class Clock:
    now = 0.0

    def __call__(self):
        return self.now


async def clean(db):
    await db.execute("DELETE FROM machine_events WHERE device_id LIKE 'apim-%'")
    await db.execute("DELETE FROM devices WHERE fleet_id = $1", FLEET)
    await db.execute("DELETE FROM fleets WHERE id = $1", FLEET)


@pytest.fixture
async def db(test_env):
    conn = await asyncpg.connect(os.environ["DATABASE_URL"])
    await clean(conn)
    await conn.execute("INSERT INTO fleets (id, name) VALUES ($1, 'Machines test')", FLEET)
    await conn.execute(
        """INSERT INTO devices (id, fleet_id, type_id, display_name, deleted_at) VALUES
           ('apim-washer', $1, 'laundry-washer', 'Washer 1', NULL),
           ('apim-other', $1, 'laundry-dryer', 'Dryer 1', NULL),
           ('apim-gone', $1, 'laundry-washer', 'Removed', now())""",
        FLEET,
    )
    yield conn
    await clean(conn)
    await conn.close()


@pytest.fixture
async def rd(test_env):
    r = redis.asyncio.Redis.from_url(os.environ["REDIS_URL"], decode_responses=True)
    yield r
    await r.aclose()


async def add_events(db, device_id, count, start_s=1793166000):
    # Inserted oldest first, one minute apart, cycling through states so from != to.
    for i in range(count):
        await db.execute(
            "INSERT INTO machine_events (device_id, from_state, to_state, at) VALUES ($1, $2, $3, to_timestamp($4))",
            device_id,
            STATES[i % 3],
            STATES[(i + 1) % 3],
            start_s + 60 * i,
        )


async def test_machine_with_live_data_and_events(db, rd, client):
    await rd.hset("device:apim-washer:state", mapping={"state": "running", "since": 1793166402160})
    await rd.set("device:apim-washer:last_seen", 1793166402)
    await rd.hset("device:apim-washer:latest", mapping={"ts": "2026-10-28T05:46:42.25Z", "vibration_rms": 0.0423})
    await add_events(db, "apim-washer", 2)
    resp = await client.get("/api/machines/apim-washer")
    assert resp.status_code == 200
    assert resp.headers["cache-control"] == "public, max-age=2"
    assert resp.json() == {
        "id": "apim-washer",
        "fleet": FLEET,
        "name": "Washer 1",
        "kind": "washer",
        "state": "running",
        "since": "2026-10-28T05:46:42.160Z",
        "online": True,
        "last_seen": "2026-10-28T05:46:42.000Z",
        "latest": {"vibration_rms": 0.0423, "ts": "2026-10-28T05:46:42.250Z"},
        "events": [
            {"from": "running", "to": "finished", "at": "2026-10-28T05:41:00.000Z"},
            {"from": "free", "to": "running", "at": "2026-10-28T05:40:00.000Z"},
        ],
    }


async def test_machine_without_any_data(db, client):
    body = (await client.get("/api/machines/apim-other")).json()
    assert (body["state"], body["online"], body["since"], body["last_seen"], body["latest"], body["events"]) == (
        "none", False, None, None, None, [],
    )
    assert (body["kind"], body["fleet"]) == ("dryer", FLEET)


async def test_events_newest_first_at_most_ten_only_this_device(db, client):
    await add_events(db, "apim-washer", 12)
    await add_events(db, "apim-other", 1, start_s=1793170000)
    events = (await client.get("/api/machines/apim-washer")).json()["events"]
    assert len(events) == 10
    assert events[0]["at"] == "2026-10-28T05:51:00.000Z"
    assert events[-1]["at"] == "2026-10-28T05:42:00.000Z"
    assert [e["at"] for e in events] == sorted((e["at"] for e in events), reverse=True)


async def test_events_at_same_time_newest_insert_first(db, client):
    for from_state, to_state in [("free", "running"), ("running", "finished")]:
        await db.execute(
            "INSERT INTO machine_events (device_id, from_state, to_state, at) VALUES ('apim-washer', $1, $2, to_timestamp(1793166000))",
            from_state,
            to_state,
        )
    events = (await client.get("/api/machines/apim-washer")).json()["events"]
    assert [e["to"] for e in events] == ["finished", "running"]


async def test_event_time_keeps_milliseconds(db, client):
    await db.execute(
        """INSERT INTO machine_events (device_id, from_state, to_state, at)
           VALUES ('apim-washer', 'free', 'running', '2026-10-28T05:46:42.999999Z')"""
    )
    events = (await client.get("/api/machines/apim-washer")).json()["events"]
    assert events[0]["at"] == "2026-10-28T05:46:42.999Z"


@pytest.mark.parametrize("device_id", ["no-such-machine", "apim-gone"])
async def test_unknown_or_deleted_is_404(db, client, device_id):
    resp = await client.get(f"/api/machines/{device_id}")
    assert resp.status_code == 404
    assert resp.json() == {"error": "machine_not_found", "message": f"No machine with ID '{device_id}'."}


async def test_machine_cached_for_two_seconds(db, rd, client):
    clock = Clock()
    app.state.cache = TTLCache(clock=clock)
    await rd.hset("device:apim-washer:state", mapping={"state": "free", "since": 1})
    assert (await client.get("/api/machines/apim-washer")).json()["state"] == "free"

    await rd.hset("device:apim-washer:state", "state", "running")
    clock.now = 1.9
    assert (await client.get("/api/machines/apim-washer")).json()["state"] == "free"
    clock.now = 2.0
    assert (await client.get("/api/machines/apim-washer")).json()["state"] == "running"


async def test_machines_do_not_share_cache_entries(db, client):
    assert (await client.get("/api/machines/apim-washer")).json()["id"] == "apim-washer"
    assert (await client.get("/api/machines/apim-other")).json()["id"] == "apim-other"
    assert (await client.get("/api/machines/no-such-machine")).status_code == 404
