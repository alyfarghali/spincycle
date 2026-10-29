from collections.abc import AsyncIterator
from contextlib import asynccontextmanager

from fastapi import FastAPI


@asynccontextmanager
async def lifespan(app: FastAPI) -> AsyncIterator[None]:
    raise NotImplementedError
    yield


def create_app() -> FastAPI:
    raise NotImplementedError


app = create_app()
