import asyncio
import bisect
import itertools
import time

from fastapi import APIRouter, Query, Request, Response

from app import model
from app.model import Fleet, State
from app.routes.errors import fleet_not_found
from app.store import postgres, redis

router = APIRouter(prefix="/api/fleets/{fleet_id}")


async def load_fleet(request: Request, fleet_id: str) -> Fleet:
    state = request.app.state
    fleet = await state.cache.get(f"fleet:{fleet_id}", 60, lambda: postgres.fleet(state.pg, fleet_id))
    if fleet is None:
        raise fleet_not_found(fleet_id)
    return fleet


async def load_states(request: Request, fleet_id: str) -> dict[str, tuple[str, int]]:
    state = request.app.state
    return await state.cache.get(f"states:{fleet_id}", 2, lambda: redis.fleet_states(state.redis, fleet_id))


@router.get("/summary")
async def summary(fleet_id: str, request: Request, response: Response) -> dict:
    r = request.app.state.redis

    async def load() -> dict:
        fleet = await load_fleet(request, fleet_id)
        # Wall time, not monotonic: the rate keys are named by Unix second.
        now = time.time()
        counts, readings, transitions = await asyncio.gather(
            redis.fleet_counts(r, fleet_id),
            redis.rates(r, fleet_id, "readings", int(now)),
            redis.rates(r, fleet_id, "transitions", int(now)),
        )
        devices = len(fleet.devices)
        counts = {s: counts.get(s, 0) for s in model.STATE_CODES if s != "none"}
        # max(0, ...): a device deleted in Postgres may still be counted in Redis.
        counts["none"] = max(0, devices - sum(counts.values()))
        return {
            "fleet": fleet.id,
            "name": fleet.name,
            "devices": devices,
            "online": sum(n for s, n in counts.items() if model.is_online(s)),
            "counts": counts,
            "readings_per_s": readings,
            "transitions_per_s": transitions,
            "generated_at": model.iso_ms(int(now * 1000)),
        }

    body = await request.app.state.cache.get(f"summary:{fleet_id}", 2, load)
    response.headers["Cache-Control"] = "public, max-age=2"
    return body


@router.get("/devices")
async def devices(fleet_id: str, request: Request, response: Response) -> dict:
    fleet = await load_fleet(request, fleet_id)
    response.headers["Cache-Control"] = "public, max-age=60"
    return {
        "fleet": fleet.id,
        "version": fleet.version,
        "devices": [{"id": d.id, "name": d.name, "kind": model.kind(d.type_id)} for d in fleet.devices],
    }


@router.get("/states")
async def states(fleet_id: str, request: Request, response: Response) -> dict:
    fleet = await load_fleet(request, fleet_id)
    live = await load_states(request, fleet_id)
    response.headers["Cache-Control"] = "public, max-age=2"
    return {"fleet": fleet.id, "version": fleet.version, "states": model.states_string(fleet.devices, live)}


@router.get("/machines")
async def machines(
    fleet_id: str,
    request: Request,
    response: Response,
    state: State | None = None,
    limit: int = Query(100, ge=1, le=500),
    after: str | None = None,
) -> dict:
    async def load() -> dict:
        fleet = await load_fleet(request, fleet_id)
        start = bisect.bisect_right(fleet.devices, after, key=lambda d: d.id) if after else 0
        candidates = iter(fleet.devices[start:])
        if state:
            states = await load_states(request, fleet_id)
            candidates = (d for d in candidates if states.get(d.id, ("none", 0))[0] == state)
        # One extra tells us whether there's a next page without a separate count.
        page = list(itertools.islice(candidates, limit + 1))
        has_next = len(page) > limit
        page = page[:limit]
        lives = await redis.live(request.app.state.redis, [d.id for d in page])
        return {
            "fleet": fleet.id,
            "machines": [model.machine_json(d, live) for d, live in zip(page, lives)],
            "next": page[-1].id if has_next else None,
        }

    body = await request.app.state.cache.get(f"machines:{fleet_id}:{state}:{limit}:{after}", 2, load)
    response.headers["Cache-Control"] = "public, max-age=2"
    return body
