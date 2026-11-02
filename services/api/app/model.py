from dataclasses import dataclass
from typing import Literal
from datetime import UTC, datetime
import hashlib

State = Literal["free", "running", "finished", "offline", "unknown", "none"]

STATE_CODES: dict[str, str] = {
    "free": "f",
    "running": "r",
    "finished": "d",
    "offline": "o",
    "unknown": "u",
    "none": "-",
}
_KINDS = {"laundry-washer": "washer", "laundry-dryer": "dryer"}


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
    return _KINDS.get(type_id, type_id)


def is_online(state: str) -> bool:
    return state in ("free", "running", "finished", "unknown")


def _format(t: datetime) -> str:
    return t.isoformat(timespec="milliseconds").replace("+00:00", "Z")


def iso_ms(unix_ms: int) -> str:
    return _format(datetime.fromtimestamp(unix_ms / 1000, UTC))


def normalize_ts(rfc3339: str) -> str:
    return _format(datetime.fromisoformat(rfc3339))


def version(device_ids: list[str]) -> str:
    return hashlib.sha1("\n".join(device_ids).encode()).hexdigest()[:8]


def states_string(devices: list[Device], states: dict[str, tuple[str, int]]) -> str:
    return "".join(STATE_CODES.get(states.get(d.id, ("none", 0))[0], "-") for d in devices)


def _latest_json(latest: dict[str, str]) -> dict | None:
    if not latest:
        return None
    metrics = {k: float(v) for k, v in latest.items() if k not in ("ts", "boot", "seq")}
    return {**metrics, "ts": normalize_ts(latest["ts"])}


def machine_json(device: Device, live: Live) -> dict:
    state = live.state or "none"
    return {
        "id": device.id,
        "name": device.name,
        "kind": kind(device.type_id),
        "state": state,
        "since": None if live.since_ms is None else iso_ms(live.since_ms),
        "online": is_online(state),
        "last_seen": None if live.last_seen_s is None else iso_ms(live.last_seen_s * 1000),
        "latest": _latest_json(live.latest),
    }
