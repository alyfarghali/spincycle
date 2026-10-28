from fastapi import APIRouter, Query, Request, Response

from app.model import Fleet, State

router = APIRouter(prefix="/api/fleets/{fleet_id}")


async def load_fleet(request: Request, fleet_id: str) -> Fleet:
    raise NotImplementedError


async def load_states(request: Request, fleet_id: str) -> dict[str, tuple[str, int]]:
    raise NotImplementedError


@router.get("/summary")
async def summary(fleet_id: str, request: Request, response: Response) -> dict:
    raise NotImplementedError


@router.get("/devices")
async def devices(fleet_id: str, request: Request, response: Response) -> dict:
    raise NotImplementedError


@router.get("/states")
async def states(fleet_id: str, request: Request, response: Response) -> dict:
    raise NotImplementedError


@router.get("/machines")
async def machines(
    fleet_id: str,
    request: Request,
    response: Response,
    state: State | None = None,
    limit: int = Query(100, ge=1, le=500),
    after: str | None = None,
) -> dict:
    raise NotImplementedError
