import pytest

from app import model


@pytest.mark.parametrize(
    ("type_id", "expected"),
    [
        ("laundry-washer", "washer"),
        ("laundry-dryer", "dryer"),
        ("loadtest-sim", "loadtest-sim"),
        ("anything-new", "anything-new"),
    ],
)
def test_kind(type_id, expected):
    assert model.kind(type_id) == expected


@pytest.mark.parametrize(
    ("state", "expected"),
    [
        ("free", True),
        ("running", True),
        ("finished", True),
        ("unknown", True),
        ("offline", False),
        ("none", False),
        ("", False),
    ],
)
def test_is_online(state, expected):
    assert model.is_online(state) is expected


@pytest.mark.parametrize(
    ("unix_ms", "expected"),
    [
        (0, "1970-01-01T00:00:00.000Z"),
        (1793166402160, "2026-10-28T05:46:42.160Z"),
        (1793166402000, "2026-10-28T05:46:42.000Z"),
        (1793166402999, "2026-10-28T05:46:42.999Z"),
        (1709210096789, "2024-02-29T12:34:56.789Z"),
    ],
)
def test_iso_ms(unix_ms, expected):
    assert model.iso_ms(unix_ms) == expected


def test_iso_ms_keeps_every_millisecond():
    # Dividing by 1000 goes through a float, which must never lose a millisecond when formatted.
    base = 1793166402000
    for ms in range(1000):
        assert model.iso_ms(base + ms).endswith(f".{ms:03d}Z")


@pytest.mark.parametrize(
    ("rfc3339", "expected"),
    [
        ("2026-10-28T05:46:42Z", "2026-10-28T05:46:42.000Z"),
        ("2026-10-28T05:46:42.25Z", "2026-10-28T05:46:42.250Z"),
        ("2026-10-28T05:46:42.250Z", "2026-10-28T05:46:42.250Z"),
        ("2026-10-28T05:46:42.999999999Z", "2026-10-28T05:46:42.999Z"),
    ],
)
def test_normalize_ts(rfc3339, expected):
    assert model.normalize_ts(rfc3339) == expected


def test_version():
    ids = ["a", "b", "c"]
    assert model.version(ids) == model.version(["a", "b", "c"])
    assert len(model.version(ids)) == 8
    assert model.version(ids) != model.version(["a", "b"])
    assert model.version(ids) != model.version(["b", "a", "c"])
    assert model.version(["ab", "c"]) != model.version(["a", "bc"])


def dev(id, type_id="laundry-washer"):
    return model.Device(id=id, fleet_id="f", name=f"Name {id}", type_id=type_id)


def test_states_string_all_states_in_device_order():
    names = ["free", "running", "finished", "offline", "unknown", "none"]
    devices = [dev(n) for n in names]
    states = {n: (n, 1) for n in reversed(names)}
    assert model.states_string(devices, states) == "frdou-"


def test_states_string_missing_and_unrecognised_are_none():
    devices = [dev("a"), dev("b"), dev("c")]
    assert model.states_string(devices, {"a": ("running", 1), "c": ("banana", 2)}) == "r--"


def test_states_string_empty_and_one_char_per_device():
    assert model.states_string([], {}) == ""
    assert len(model.states_string([dev(str(i)) for i in range(10000)], {})) == 10000


def test_machine_json_no_live_data():
    assert model.machine_json(dev("a"), model.Live("", None, None, {})) == {
        "id": "a",
        "name": "Name a",
        "kind": "washer",
        "state": "none",
        "since": None,
        "online": False,
        "last_seen": None,
        "latest": None,
    }


def test_machine_json_full():
    live = model.Live(
        "running",
        1793166402160,
        1793166402,
        {"ts": "2026-10-28T05:46:42.25Z", "boot": "12", "seq": "1842", "vibration_rms": "0.0423"},
    )
    assert model.machine_json(dev("a", "laundry-dryer"), live) == {
        "id": "a",
        "name": "Name a",
        "kind": "dryer",
        "state": "running",
        "since": "2026-10-28T05:46:42.160Z",
        "online": True,
        "last_seen": "2026-10-28T05:46:42.000Z",
        "latest": {"vibration_rms": 0.0423, "ts": "2026-10-28T05:46:42.250Z"},
    }


def test_machine_json_zero_is_a_time_not_missing():
    out = model.machine_json(dev("a"), model.Live("free", 0, 0, {}))
    assert out["since"] == "1970-01-01T00:00:00.000Z"
    assert out["last_seen"] == "1970-01-01T00:00:00.000Z"


def test_machine_json_offline_is_not_online():
    assert model.machine_json(dev("a"), model.Live("offline", 1, 1, {}))["online"] is False
