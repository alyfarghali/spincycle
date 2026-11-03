import logging

import asyncpg
from fastapi import FastAPI, Request
from fastapi.exceptions import RequestValidationError
from fastapi.responses import JSONResponse
from redis.exceptions import RedisError

logger = logging.getLogger(__name__)


class ApiError(Exception):
    def __init__(self, status: int, error: str, message: str) -> None:
        super().__init__(message)
        self.status = status
        self.error = error
        self.message = message


# OSError covers refused connections and TimeoutError. OperatorInterventionError is Postgres
# shutting down or starting up, InsufficientResourcesError is it being out of connections.
UNAVAILABLE: tuple[type[Exception], ...] = (
    OSError,
    asyncpg.PostgresConnectionError,
    asyncpg.OperatorInterventionError,
    asyncpg.InsufficientResourcesError,
    asyncpg.InterfaceError,
    RedisError,
)


def fleet_not_found(fleet_id: str) -> ApiError:
    return ApiError(404, "fleet_not_found", f"No fleet with ID '{fleet_id}'.")


def machine_not_found(device_id: str) -> ApiError:
    return ApiError(404, "machine_not_found", f"No machine with ID '{device_id}'.")


def _response(status: int, error: str, message: str) -> JSONResponse:
    return JSONResponse({"error": error, "message": message}, status_code=status)


async def api_error_handler(request: Request, exc: ApiError) -> JSONResponse:
    return _response(exc.status, exc.error, exc.message)


async def validation_error_handler(request: Request, exc: RequestValidationError) -> JSONResponse:
    first = exc.errors()[0]
    where = ".".join(str(part) for part in first["loc"])
    return _response(400, "invalid_query", f"{where}: {first['msg']}")


async def unavailable_handler(request: Request, exc: Exception) -> JSONResponse:
    # The client message hides the cause, so the log has to keep it.
    logger.error("database unavailable", exc_info=exc)
    return _response(503, "unavailable", "A database is unreachable. Try again shortly.")


def install(app: FastAPI) -> None:
    app.add_exception_handler(ApiError, api_error_handler)
    app.add_exception_handler(RequestValidationError, validation_error_handler)
    for exc_type in UNAVAILABLE:
        app.add_exception_handler(exc_type, unavailable_handler)
