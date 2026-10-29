from fastapi import Request
from fastapi.exceptions import RequestValidationError
from fastapi.responses import JSONResponse


class ApiError(Exception):
    def __init__(self, status: int, error: str, message: str) -> None:
        super().__init__(message)
        self.status = status
        self.error = error
        self.message = message


UNAVAILABLE: tuple[type[Exception], ...] = ()


def fleet_not_found(fleet_id: str) -> ApiError:
    raise NotImplementedError


def machine_not_found(device_id: str) -> ApiError:
    raise NotImplementedError


async def api_error_handler(request: Request, exc: ApiError) -> JSONResponse:
    raise NotImplementedError


async def validation_error_handler(request: Request, exc: RequestValidationError) -> JSONResponse:
    raise NotImplementedError


async def unavailable_handler(request: Request, exc: Exception) -> JSONResponse:
    raise NotImplementedError
