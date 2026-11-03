from typing import Literal

from redis.asyncio import Redis

from app.model import Live


def connect(url: str) -> Redis:
    # Short timeouts make a dead Redis a quick 503, not a page that hangs.
    return Redis.from_url(url, decode_responses=True, socket_timeout=0.5, socket_connect_timeout=0.5)


async def ping(r: Redis) -> None:
    await r.ping()


async def fleet_states(r: Redis, fleet_id: str) -> dict[str, tuple[str, int]]:
    entries = await r.hgetall(f"fleet:{fleet_id}:state")
    states = {}
    for device_id, value in entries.items():
        state, since_ms = value.split("|", 1)
        states[device_id] = (state, int(since_ms))
    return states


async def fleet_counts(r: Redis, fleet_id: str) -> dict[str, int]:
    return {state: int(n) for state, n in (await r.hgetall(f"fleet:{fleet_id}:counts")).items()}


async def rates(r: Redis, fleet_id: str, kind: Literal["readings", "transitions"], now_s: int) -> list[int]:
    # Stops before now_s: the current second is still being counted and would always look low.
    values = await r.mget([f"fleet:{fleet_id}:{kind}:{s}" for s in range(now_s - 60, now_s)])
    return [int(v) if v else 0 for v in values]


async def live(r: Redis, device_ids: list[str]) -> list[Live]:
    # One round trip for the whole page instead of three per device.
    async with r.pipeline(transaction=False) as p:
        for device_id in device_ids:
            p.hgetall(f"device:{device_id}:state")
            p.get(f"device:{device_id}:last_seen")
            p.hgetall(f"device:{device_id}:latest")
        results = await p.execute()
    machines = []
    for i in range(0, len(results), 3):
        state, last_seen, latest = results[i : i + 3]
        machines.append(
            Live(
                state.get("state", ""),
                int(state["since"]) if "since" in state else None,
                int(last_seen) if last_seen is not None else None,
                latest,
            )
        )
    return machines
