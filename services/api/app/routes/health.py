import asyncio
from collections.abc import Awaitable

from fastapi import APIRouter, Request
from fastapi.responses import JSONResponse

from app.routes.errors import UNAVAILABLE
from app.store import postgres, redis

router = APIRouter()

CHECK_TIMEOUT_S = 1.0


async def _check(ping: Awaitable[None]) -> str:
    try:
        # A hung database must read as down: an uptime check that hangs tells you nothing.
        async with asyncio.timeout(CHECK_TIMEOUT_S):
            await ping
        return "ok"
    except UNAVAILABLE:
        return "down"


@router.get("/health")
async def health(request: Request) -> JSONResponse:
    pg = await _check(postgres.ping(request.app.state.pg))
    rd = await _check(redis.ping(request.app.state.redis))
    ok = pg == "ok" and rd == "ok"
    return JSONResponse(
        {"status": "ok" if ok else "down", "postgres": pg, "redis": rd},
        status_code=200 if ok else 503,
    )
