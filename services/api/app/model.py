from dataclasses import dataclass
from typing import Literal

State = Literal["free", "running", "finished", "offline", "unknown", "none"]

STATE_CODES: dict[str, str] = {}


@dataclass(frozen=True)
class Device:
    id: str
    fleet_id: str
    name: str
    type_id: str


@dataclass(frozen=True)
class Fleet:
    id: str
    name: str
    devices: list[Device]
    version: str


@dataclass(frozen=True)
class Live:
    state: str
    since_ms: int | None
    last_seen_s: int | None
    latest: dict[str, str]


def kind(type_id: str) -> str:
    raise NotImplementedError


def is_online(state: str) -> bool:
    raise NotImplementedError


def iso_ms(unix_ms: int) -> str:
    raise NotImplementedError


def normalize_ts(rfc3339: str) -> str:
    raise NotImplementedError


def version(device_ids: list[str]) -> str:
    raise NotImplementedError


def states_string(devices: list[Device], states: dict[str, tuple[str, int]]) -> str:
    raise NotImplementedError


def machine_json(device: Device, live: Live) -> dict:
    raise NotImplementedError
