import asyncpg

from app.model import Device, Fleet


async def connect(url: str) -> asyncpg.Pool:
    raise NotImplementedError


async def ping(pool: asyncpg.Pool) -> None:
    raise NotImplementedError


async def fleet(pool: asyncpg.Pool, fleet_id: str) -> Fleet | None:
    raise NotImplementedError


async def device(pool: asyncpg.Pool, device_id: str) -> Device | None:
    raise NotImplementedError


async def recent_events(pool: asyncpg.Pool, device_id: str, limit: int = 10) -> list[dict]:
    raise NotImplementedError
