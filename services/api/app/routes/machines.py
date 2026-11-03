import asyncio

from fastapi import APIRouter, Request, Response

from app import model
from app.routes.errors import machine_not_found
from app.store import postgres, redis

router = APIRouter(prefix="/api/machines")


@router.get("/{device_id}")
async def machine(device_id: str, request: Request, response: Response) -> dict:
    state = request.app.state

    async def load() -> dict | None:
        device = await postgres.device(state.pg, device_id)
        if device is None:
            return None
        (live,), events = await asyncio.gather(
            redis.live(state.redis, [device_id]), postgres.recent_events(state.pg, device_id)
        )
        return {**model.machine_json(device, live), "fleet": device.fleet_id, "events": events}

    body = await state.cache.get(f"machine:{device_id}", 2, load)
    if body is None:
        raise machine_not_found(device_id)
    response.headers["Cache-Control"] = "public, max-age=2"
    return body
