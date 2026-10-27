# State engine

Decides whether each machine is **free**, **running**, **finished** or **offline** from its stream of
`vibration_rms` readings. It's pure: every function takes the machine's current state and an input
and returns the new state plus any transitions. No database, Redis or clock inside, so every rule is a
table test and recorded cycles can be replayed through the same code ingest runs.

Status: the pure engine and its tests are done. `runner.go` (applying transitions to Postgres/Redis
inside ingest) is not written yet.

## Files

| File | Contents |
|---|---|
| `states.go` | `State`, `Machine`, `Transition` |
| `params.go` | `Params`, `ParseParams`, `Validate` |
| `engine.go` | `OnReading`, `OnTick`, `OnOffline` and their helpers |
| `runner.go` | *stub* — will hold per-device state in ingest, write `machine_events`, publish to Redis |
| `engine_test.go` | Transition table tests and config parsing tests |
| `replay_test.go` | Replays cycle CSVs and checks contract C2 |

## Types

**`State`**: `free`, `running`, `finished`, `offline`, and `unknown` — back after too long offline to
trust the old state, so it's resolved from new readings.

**`Machine`** — one machine's state. The zero value is a device never seen before.

| Field | Meaning |
|---|---|
| `State`, `Since` | Current state and when it began |
| `LastReading` | Timestamp of the newest reading applied; older readings are ignored |
| `ShakingSince` | Start of the current run of shaking readings (zero if none) |
| `QuietSince` | Start of the current run of quiet readings (zero if none) |
| `Prev`, `PrevSince` | State and its start time saved on going offline, so a short drop resumes where it left off |

**`Transition`** — `{From, To, At}`. Ingest will write one `machine_events` row per transition.

## Parameters

Loaded from `device_types.config`, overridden per device by `devices.config`. Durations are in seconds.
There are no defaults in code: `ParseParams` fails if any key is missing. Other keys (e.g. `interval_s`)
are ignored.

| Key | Meaning |
|---|---|
| `on_threshold` | RMS (g) at or above: **shaking** |
| `off_threshold` | RMS (g) at or below: **quiet**. Must be below `on_threshold` |
| `start_confirm` | Shaking this long → running |
| `stop_confirm` | Quiet this long → finished. Must outlast wash/soak pauses |
| `clear_after` | Finished this long → free |
| `offline_after` | No reading this long → offline |
| `resume_window` | Back within this → previous state restored; later → `unknown` |

## Functions

| Function | Input | Does |
|---|---|---|
| `OnReading(m, rms, t, p)` | One reading taken at `t` | Resumes or resolves after offline, applies `clear_after`, updates the shaking/quiet runs, then applies the transition rules below |
| `OnTick(m, now, p)` | Time passing with no reading | `clear_after` and `offline_after`. Ingest's sweeper will call it every 30 s |
| `OnOffline(m, at)` | Last Will `offline` (or the device's own before a clean disconnect) | → offline, saving the current state |

## Rules

| From | To | When |
|---|---|---|
| free, finished | running | Shaking continuously for `start_confirm` |
| running | finished | Quiet continuously for `stop_confirm` |
| finished | free | `clear_after` elapsed (stamped at the exact expiry, not when noticed) |
| any | offline | Last Will, or no reading for `offline_after` |
| offline | previous state | Next reading within `resume_window` of the last one; its timer is restored too |
| offline | unknown | Next reading after `resume_window` |
| unknown | running | Shaking for `start_confirm` |
| unknown | finished / free | Quiet for `start_confirm`: **finished** if it was running or finished before the drop, otherwise free |

## Design decisions

- **Hysteresis.** A reading between the two thresholds resets neither run, so a value hovering near
  one threshold can't flip the machine back and forth.
- **Resume after offline** (CLAUDE.md §6.3 fix, option 1). A running machine whose sensor drops off
  Wi-Fi briefly comes back as running, never free, and its subscribers still get "finished" later.
- **`unknown` resolves to finished, not free, after a mid-cycle drop.** Extends "never a wrong free"
  to long gaps: the load may still be inside, and subscribers are still alerted.
- **A gap resets both runs.** No data is no evidence of shaking or quiet, so a long dropout can't
  finish a cycle that was still running.
- **Late readings don't rewind state.** Delivery is unordered (contract A8). A reading older than the
  newest applied one is stored by ingest but ignored here.

## Tests

```sh
go test ./services/ingest/engine -v
```

- `engine_test.go`, each test against a spec rule (placeholder thresholds until recordings exist):
  - `TestTransitions` — one row per scenario: short bump, 30 s start, 4 min soak pause, 5 min finish,
    `clear_after`, hysteresis both ways (doesn't start, doesn't reset, doesn't count as quiet), a quiet run
    interrupted by shaking, a new cycle before `clear_after`, silence → offline, short and long Wi-Fi drops
    mid-cycle, long drop while idle, `unknown` → running, short drop while finished, late reading, and the
    CLAUDE.md §8 failure modes: neighbour shaking in short bursts, reboot mid-cycle with and without the sweeper.
  - `TestTransitionTimes` — every transition's `At` (e.g. running at the reading that completes `start_confirm`).
  - `TestClearAfterTimestamp` — finished → free is stamped at the expiry, not when noticed.
  - `TestOfflineEdgeCases` — Last Will or tick for a never-seen device; repeated offline signals give one transition.
  - `TestResumeWindowBoundary` — back exactly at `resume_window` resumes; 5 s later is `unknown`.
  - `TestDuplicateReadingIgnored` — a reading with the same timestamp changes nothing (contract A8 redelivery).
  - `TestParseParams` — merge and override, no defaults, every invalid value, non-engine keys ignored.
- `replay_test.go` — every CSV in `testdata/synthetic/` and `testdata/cycles/` (columns
  `ts,vibration_rms,event`). Checks contract C2: running within `start_confirm` + 60 s of `start`,
  finished within `stop_confirm` + 60 s of `end`, never finished or free in between.
- `testdata/synthetic/` holds made-up cycles until real recordings land in `testdata/cycles/`
  (which is for real recordings only).

Proven able to fail (mutation checks): a too-short `stop_confirm` (3 min) fails the replay on the soak pause;
an exclusive resume window fails `TestResumeWindowBoundary`; removing hysteresis fails both hysteresis rows;
resolving `unknown` to free after a mid-cycle drop fails the long-drop row.
