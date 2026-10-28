from dataclasses import dataclass


@dataclass(frozen=True)
class Settings:
    database_url: str
    redis_url: str


def load() -> Settings:
    raise NotImplementedError
