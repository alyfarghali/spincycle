from fastapi import APIRouter, Request, Response

router = APIRouter(prefix="/api/machines")


@router.get("/{device_id}")
async def machine(device_id: str, request: Request, response: Response) -> dict:
    raise NotImplementedError
