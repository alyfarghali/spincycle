import os

import asyncpg
import pytest
import redis.asyncio

from app.cache import TTLCache
from app.main import app
from app.routes import fleets
from app.model import version

FLEET = "apitest"
IDS = ["api-dryer", "api-sim", "api-washer"]


class Clock:
    now = 0.0

    def __call__(self):
        return self.now


async def clean(db):
    await db.execute("DELETE FROM devices WHERE fleet_id = $1", FLEET)
    await db.execute("DELETE FROM fleets WHERE id = $1", FLEET)


@pytest.fixture
async def db(test_env):
    conn = await asyncpg.connect(os.environ["DATABASE_URL"])
    await clean(conn)
    await conn.execute("INSERT INTO fleets (id, name) VALUES ($1, 'API test')", FLEET)
    # Not in ID order: the API must sort, not rely on insertion order.
    await conn.execute(
        """INSERT INTO devices (id, fleet_id, type_id, display_name, deleted_at) VALUES
           ('api-sim', $1, 'loadtest-sim', 'Sim', NULL),
           ('api-washer', $1, 'laundry-washer', 'Washer 1', NULL),
           ('api-dryer', $1, 'laundry-dryer', 'Dryer 1', NULL),
           ('api-gone', $1, 'laundry-washer', 'Removed', now())""",
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


async def test_devices_sorted_without_deleted_with_kinds(db, client):
    resp = await client.get(f"/api/fleets/{FLEET}/devices")
    assert resp.status_code == 200
    assert resp.json() == {
        "fleet": FLEET,
        "version": version(IDS),
        "devices": [
            {"id": "api-dryer", "name": "Dryer 1", "kind": "dryer"},
            {"id": "api-sim", "name": "Sim", "kind": "loadtest-sim"},
            {"id": "api-washer", "name": "Washer 1", "kind": "washer"},
        ],
    }
    assert resp.headers["cache-control"] == "public, max-age=60"


async def test_states_align_with_devices(db, rd, client):
    await rd.hset(
        f"fleet:{FLEET}:state",
        mapping={"api-washer": "running|1793166402160", "api-dryer": "finished|1793166402161"},
    )
    devices = (await client.get(f"/api/fleets/{FLEET}/devices")).json()
    resp = await client.get(f"/api/fleets/{FLEET}/states")
    assert resp.status_code == 200
    body = resp.json()
    # api-sim has never reported, so it's the "none" character.
    assert body == {"fleet": FLEET, "version": devices["version"], "states": "d-r"}
    assert [d["id"] for d in devices["devices"]] == IDS
    assert resp.headers["cache-control"] == "public, max-age=2"


async def test_deleted_device_in_redis_does_not_shift_the_string(db, rd, client):
    await rd.hset(f"fleet:{FLEET}:state", mapping={"api-gone": "running|1", "api-sim": "free|1"})
    body = (await client.get(f"/api/fleets/{FLEET}/states")).json()
    assert body["states"] == "-f-"


@pytest.mark.parametrize("path", ["summary", "devices", "states", "machines"])
async def test_unknown_fleet_is_404(client, path):
    resp = await client.get(f"/api/fleets/no-such-fleet/{path}")
    assert resp.status_code == 404
    assert resp.json() == {"error": "fleet_not_found", "message": "No fleet with ID 'no-such-fleet'."}


async def test_empty_fleet_returns_empty_state_string(db, client):
    await db.execute("DELETE FROM devices WHERE fleet_id = $1", FLEET)
    body = (await client.get(f"/api/fleets/{FLEET}/states")).json()
    assert body["states"] == ""


async def test_states_cached_for_two_seconds(db, rd, client):
    clock = Clock()
    app.state.cache = TTLCache(clock=clock)
    await rd.hset(f"fleet:{FLEET}:state", "api-sim", "free|1")
    assert (await client.get(f"/api/fleets/{FLEET}/states")).json()["states"] == "-f-"

    await rd.hset(f"fleet:{FLEET}:state", "api-sim", "running|2")
    clock.now = 1.9
    assert (await client.get(f"/api/fleets/{FLEET}/states")).json()["states"] == "-f-"
    clock.now = 2.0
    assert (await client.get(f"/api/fleets/{FLEET}/states")).json()["states"] == "-r-"


async def test_device_list_cached_for_sixty_seconds_then_version_changes(db, client):
    clock = Clock()
    app.state.cache = TTLCache(clock=clock)
    before = (await client.get(f"/api/fleets/{FLEET}/devices")).json()

    await db.execute(
        "INSERT INTO devices (id, fleet_id, type_id, display_name) VALUES ('api-new', $1, 'laundry-washer', 'New')",
        FLEET,
    )
    clock.now = 59.9
    assert (await client.get(f"/api/fleets/{FLEET}/devices")).json() == before

    clock.now = 60.0
    after = (await client.get(f"/api/fleets/{FLEET}/devices")).json()
    assert after["version"] != before["version"]
    states = (await client.get(f"/api/fleets/{FLEET}/states")).json()
    assert states["version"] == after["version"]
    assert len(states["states"]) == len(after["devices"]) == 4


# Fractional on purpose: the rate window must count back from the whole second.
NOW = 1793166402.75
NOW_S = 1793166402


@pytest.fixture
def frozen_time(monkeypatch):
    monkeypatch.setattr(fleets.time, "time", lambda: NOW)


async def test_summary_counts_sum_to_devices(db, rd, client, frozen_time):
    await rd.hset(f"fleet:{FLEET}:counts", mapping={"running": 1, "offline": 1})
    resp = await client.get(f"/api/fleets/{FLEET}/summary")
    assert resp.status_code == 200
    assert resp.json() == {
        "fleet": FLEET,
        "name": "API test",
        "devices": 3,
        "online": 1,
        "counts": {"free": 0, "running": 1, "finished": 0, "offline": 1, "unknown": 0, "none": 1},
        "readings_per_s": [0] * 60,
        "transitions_per_s": [0] * 60,
        "generated_at": "2026-10-28T05:46:42.750Z",
    }
    assert resp.headers["cache-control"] == "public, max-age=2"


async def test_summary_online_counts_all_four_online_states(db, rd, client):
    await rd.hset(f"fleet:{FLEET}:counts", mapping={"free": 1, "running": 1, "finished": 1, "unknown": 1, "offline": 5})
    body = (await client.get(f"/api/fleets/{FLEET}/summary")).json()
    assert body["online"] == 4


async def test_summary_none_never_negative(db, rd, client):
    # A deleted device still counted in Redis makes the counts exceed the device list.
    await rd.hset(f"fleet:{FLEET}:counts", mapping={"running": 4})
    body = (await client.get(f"/api/fleets/{FLEET}/summary")).json()
    assert body["counts"]["none"] == 0


async def test_summary_rates_are_last_sixty_complete_seconds(db, rd, client, frozen_time):
    await rd.mset({
        f"fleet:{FLEET}:readings:{NOW_S - 60}": 3,
        f"fleet:{FLEET}:readings:{NOW_S - 30}": 5,
        f"fleet:{FLEET}:readings:{NOW_S - 1}": 7,
        f"fleet:{FLEET}:readings:{NOW_S}": 99,
        f"fleet:{FLEET}:readings:{NOW_S - 61}": 99,
        f"fleet:{FLEET}:transitions:{NOW_S - 1}": 2,
    })
    body = (await client.get(f"/api/fleets/{FLEET}/summary")).json()
    readings, transitions = body["readings_per_s"], body["transitions_per_s"]
    assert len(readings) == len(transitions) == 60
    assert (readings[0], readings[30], readings[-1], sum(readings)) == (3, 5, 7, 15)
    assert (transitions[-1], sum(transitions)) == (2, 2)


async def test_summary_cached_for_two_seconds(db, rd, client):
    clock = Clock()
    app.state.cache = TTLCache(clock=clock)
    await rd.hset(f"fleet:{FLEET}:counts", "running", 1)
    assert (await client.get(f"/api/fleets/{FLEET}/summary")).json()["counts"]["running"] == 1

    await rd.hset(f"fleet:{FLEET}:counts", "running", 2)
    clock.now = 1.9
    assert (await client.get(f"/api/fleets/{FLEET}/summary")).json()["counts"]["running"] == 1
    clock.now = 2.0
    assert (await client.get(f"/api/fleets/{FLEET}/summary")).json()["counts"]["running"] == 2


def machines_url(query=""):
    return f"/api/fleets/{FLEET}/machines?{query}"


async def test_machines_pages_cover_every_device_once(db, client):
    first = (await client.get(machines_url("limit=2"))).json()
    assert [m["id"] for m in first["machines"]] == ["api-dryer", "api-sim"]
    assert first["next"] == "api-sim"
    second = (await client.get(machines_url(f"limit=2&after={first['next']}"))).json()
    assert [m["id"] for m in second["machines"]] == ["api-washer"]
    assert second["next"] is None


async def test_machines_full_page_without_more_has_no_next(db, client):
    body = (await client.get(machines_url("limit=3"))).json()
    assert len(body["machines"]) == 3
    assert body["next"] is None


async def test_machines_after_an_id_that_is_not_a_device(db, client):
    body = (await client.get(machines_url("after=api-e"))).json()
    assert [m["id"] for m in body["machines"]] == ["api-sim", "api-washer"]


async def test_machines_paging_follows_byte_order(db, client):
    # Locale sorting skips "-" at first, putting "apia" before "api-dryer"; byte order puts it after.
    await db.execute(
        """INSERT INTO devices (id, fleet_id, type_id, display_name) VALUES
           ('apia', $1, 'laundry-washer', 'A'), ('api0', $1, 'laundry-washer', '0'),
           ('apiz', $1, 'laundry-washer', 'Z')""",
        FLEET,
    )
    seen, after = [], None
    # Bounded, so a cursor that never advances fails instead of looping forever.
    for _ in range(10):
        body = (await client.get(machines_url("limit=1" + (f"&after={after}" if after else "")))).json()
        seen += [m["id"] for m in body["machines"]]
        if (after := body["next"]) is None:
            break
    assert seen == sorted(IDS + ["apia", "api0", "apiz"])


async def test_machines_card_with_and_without_live_data(db, rd, client):
    await rd.hset("device:api-washer:state", mapping={"state": "running", "since": 1793166402160})
    await rd.set("device:api-washer:last_seen", 1793166402)
    await rd.hset(
        "device:api-washer:latest",
        mapping={"ts": "2026-10-28T05:46:42.25Z", "boot": 12, "seq": 1842, "vibration_rms": 0.0423},
    )
    resp = await client.get(machines_url())
    assert resp.headers["cache-control"] == "public, max-age=2"
    body = resp.json()
    assert body["fleet"] == FLEET
    dryer, _, washer = body["machines"]
    assert washer == {
        "id": "api-washer",
        "name": "Washer 1",
        "kind": "washer",
        "state": "running",
        "since": "2026-10-28T05:46:42.160Z",
        "online": True,
        "last_seen": "2026-10-28T05:46:42.000Z",
        "latest": {"vibration_rms": 0.0423, "ts": "2026-10-28T05:46:42.250Z"},
    }
    assert (dryer["state"], dryer["since"], dryer["last_seen"], dryer["latest"]) == ("none", None, None, None)


async def test_machines_state_filter(db, rd, client):
    await rd.hset(
        f"fleet:{FLEET}:state", mapping={"api-washer": "running|1", "api-dryer": "running|2", "api-sim": "free|3"}
    )
    await rd.hset(f"fleet:{FLEET}:state", "api-gone", "running|4")
    running = (await client.get(machines_url("state=running"))).json()
    assert [m["id"] for m in running["machines"]] == ["api-dryer", "api-washer"]
    first = (await client.get(machines_url("state=running&limit=1"))).json()
    assert ([m["id"] for m in first["machines"]], first["next"]) == (["api-dryer"], "api-dryer")
    assert (await client.get(machines_url("state=finished"))).json()["machines"] == []


async def test_machines_state_none_is_devices_without_state(db, rd, client):
    await rd.hset(f"fleet:{FLEET}:state", "api-sim", "free|1")
    body = (await client.get(machines_url("state=none"))).json()
    assert [m["id"] for m in body["machines"]] == ["api-dryer", "api-washer"]


async def test_machines_cache_key_includes_every_query_value(db, rd, client):
    await rd.hset(f"fleet:{FLEET}:state", "api-sim", "free|1")
    expected = {
        "": IDS,
        "state=free": ["api-sim"],
        "limit=1": ["api-dryer"],
        "after=api-dryer": ["api-sim", "api-washer"],
    }
    for query, ids in expected.items():
        assert [m["id"] for m in (await client.get(machines_url(query))).json()["machines"]] == ids


async def test_machines_cached_for_two_seconds(db, rd, client):
    clock = Clock()
    app.state.cache = TTLCache(clock=clock)
    await rd.hset("device:api-sim:state", mapping={"state": "free", "since": 1})
    state = lambda body: body["machines"][1]["state"]
    assert state((await client.get(machines_url())).json()) == "free"

    await rd.hset("device:api-sim:state", "state", "running")
    clock.now = 1.9
    assert state((await client.get(machines_url())).json()) == "free"
    clock.now = 2.0
    assert state((await client.get(machines_url())).json()) == "running"


async def test_redis_down_is_503_on_states(db, client, monkeypatch):
    await app.state.redis.aclose()
    bad = redis.asyncio.Redis.from_url("redis://localhost:1/0", socket_connect_timeout=0.2)
    app.state.redis = bad
    resp = await client.get(f"/api/fleets/{FLEET}/states")
    assert resp.status_code == 503
    assert resp.json()["error"] == "unavailable"


async def test_fleets_do_not_share_cache_entries(db, rd, client):
    other = "apitest2"
    await db.execute("DELETE FROM devices WHERE fleet_id = $1", other)
    await db.execute("DELETE FROM fleets WHERE id = $1", other)
    await db.execute("INSERT INTO fleets (id, name) VALUES ($1, 'Other')", other)
    await db.execute(
        "INSERT INTO devices (id, fleet_id, type_id, display_name) VALUES ('api-other', $1, 'laundry-washer', 'Other')",
        other,
    )
    await rd.hset(f"fleet:{FLEET}:state", "api-sim", "free|1")
    await rd.hset(f"fleet:{other}:state", "api-other", "running|1")
    await rd.hset(f"fleet:{other}:counts", "running", 1)
    try:
        assert (await client.get(f"/api/fleets/{FLEET}/states")).json()["states"] == "-f-"
        body = (await client.get(f"/api/fleets/{other}/states")).json()
        assert body["fleet"] == other
        assert body["states"] == "r"
        assert (await client.get(f"/api/fleets/{FLEET}/summary")).json()["devices"] == 3
        body = (await client.get(f"/api/fleets/{other}/summary")).json()
        assert (body["fleet"], body["devices"], body["counts"]["running"]) == (other, 1, 1)
    finally:
        await db.execute("DELETE FROM devices WHERE fleet_id = $1", other)
        await db.execute("DELETE FROM fleets WHERE id = $1", other)
