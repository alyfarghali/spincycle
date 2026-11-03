import os
from dataclasses import dataclass


@dataclass(frozen=True)
class Settings:
    database_url: str
    redis_url: str


def load() -> Settings:
    missing = [name for name in ("DATABASE_URL", "REDIS_URL") if not os.environ.get(name)]
    if missing:
        raise RuntimeError(f"missing environment variables: {', '.join(missing)}")
    return Settings(database_url=os.environ["DATABASE_URL"], redis_url=os.environ["REDIS_URL"])
