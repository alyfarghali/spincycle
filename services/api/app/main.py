from collections.abc import AsyncIterator
from contextlib import asynccontextmanager

from fastapi import FastAPI

from app import config
from app.cache import TTLCache
from app.routes import errors, fleets, health, machines
from app.store import postgres, redis


@asynccontextmanager
async def lifespan(app: FastAPI) -> AsyncIterator[None]:
    settings = config.load()
    app.state.pg = await postgres.connect(settings.database_url)
    app.state.redis = redis.connect(settings.redis_url)
    app.state.cache = TTLCache()
    yield
    await app.state.redis.aclose()
    await app.state.pg.close()


def create_app() -> FastAPI:
    app = FastAPI(lifespan=lifespan)
    errors.install(app)
    app.include_router(health.router)
    app.include_router(fleets.router)
    app.include_router(machines.router)
    return app


app = create_app()
