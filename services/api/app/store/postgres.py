import asyncpg

from app import model
from app.model import Device, Fleet


async def connect(url: str) -> asyncpg.Pool:
    return await asyncpg.create_pool(url, min_size=1, max_size=10)


async def ping(pool: asyncpg.Pool) -> None:
    await pool.fetchval("SELECT 1")


async def fleet(pool: asyncpg.Pool, fleet_id: str) -> Fleet | None:
    name = await pool.fetchval("SELECT name FROM fleets WHERE id = $1", fleet_id)
    if name is None:
        return None
    rows = await pool.fetch(
        """SELECT id, display_name, type_id FROM devices
           WHERE fleet_id = $1 AND deleted_at IS NULL
           -- Byte order, to match Python's string comparison: /machines pages with bisect.
           ORDER BY id COLLATE "C"
        """,
        fleet_id,
    )
    devices = [Device(r["id"], fleet_id, r["display_name"], r["type_id"]) for r in rows]
    return Fleet(fleet_id, name, devices, model.version([d.id for d in devices]))


async def device(pool: asyncpg.Pool, device_id: str) -> Device | None:
    row = await pool.fetchrow(
        "SELECT fleet_id, display_name, type_id FROM devices WHERE id = $1 AND deleted_at IS NULL", device_id
    )
    return None if row is None else Device(device_id, row["fleet_id"], row["display_name"], row["type_id"])


async def recent_events(pool: asyncpg.Pool, device_id: str, limit: int = 10) -> list[dict]:
    rows = await pool.fetch(
        # Milliseconds in SQL: extract() is exact numeric, a Python float timestamp can round to the wrong ms.
        """SELECT from_state, to_state, floor(extract(epoch FROM at) * 1000)::bigint AS at_ms
           FROM machine_events WHERE device_id = $1
           ORDER BY at DESC, id DESC LIMIT $2""",
        device_id,
        limit,
    )
    return [{"from": r["from_state"], "to": r["to_state"], "at": model.iso_ms(r["at_ms"])} for r in rows]
