from typing import Literal

from redis.asyncio import Redis

from app.model import Live


def connect(url: str) -> Redis:
    raise NotImplementedError


async def ping(r: Redis) -> None:
    raise NotImplementedError


async def fleet_states(r: Redis, fleet_id: str) -> dict[str, tuple[str, int]]:
    raise NotImplementedError


async def fleet_counts(r: Redis, fleet_id: str) -> dict[str, int]:
    raise NotImplementedError


async def rates(r: Redis, fleet_id: str, kind: Literal["readings", "transitions"], now_s: int) -> list[int]:
    raise NotImplementedError


async def live(r: Redis, device_ids: list[str]) -> list[Live]:
    raise NotImplementedError
