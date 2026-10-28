# api

Python (FastAPI) HTTP service behind Caddy. The contract is `docs/api.md`; the Redis keys it reads are
in `storage/redis/keys.md` (written by ingest). This first version is read-only: health, fleet summary,
device list, state string, paged machines, one machine. Notifier, admin, subscriptions and busy-times
come later (their files are still stubs).

| File | Does |
|---|---|
| `app/config.py` | Settings from environment variables |
| `app/model.py` | Pure data and formatting: types, state codes, timestamps, the JSON for one machine |
| `app/cache.py` | 2 s in-process cache with one loader per key |
| `app/store/postgres.py` | Fleets, devices, recent transitions |
| `app/store/redis.py` | Live state, counts, rates |
| `app/routes/errors.py` | `{error, message}` responses |
| `app/routes/health.py`, `fleets.py`, `machines.py` | Endpoints |
| `app/main.py` | Builds the app; opens and closes connections |

Every function body is `raise NotImplementedError` until written. Build in the order below; each step
is one branch and PR with its tests. Make each test fail once on purpose (break the code, see red) so
you know it can catch a bug.

## Setup

```sh
cd services/api
python3.12 -m venv .venv
source .venv/bin/activate
pip install -e '.[dev]'
```

Run it locally against the Compose Postgres and Redis (from the repo root `docker compose ... up -d
postgres redis`):

```sh
uvicorn app.main:app --reload --env-file ../../.env
curl -s localhost:8000/health
```

Tests: `pytest`. Route tests need the databases, using the same variables as the Go tests:
`TEST_DATABASE_URL` (from `storage/test-db.sh`) and `TEST_REDIS_URL=redis://localhost:6379/15`.
**Never point tests at Redis database 0**: they flush it.

---

## Step 1: `app/model.py` (pure, no I/O)

Everything here is a plain function, so it's tested without any database.

| Function | Write |
|---|---|
| `STATE_CODES` | The `/states` alphabet: `free→f`, `running→r`, `finished→d`, `offline→o`, `unknown→u`, `none→-`. |
| `kind(type_id)` | `laundry-washer → "washer"`, `laundry-dryer → "dryer"`, anything else → `type_id` unchanged. A dict lookup with a default. |
| `is_online(state)` | True for `free`, `running`, `finished`, `unknown`. False for `offline` and `none`. |
| `iso_ms(unix_ms)` | Unix milliseconds → `"2026-10-27T14:03:11.250Z"`. Use `datetime.fromtimestamp(unix_ms / 1000, UTC).isoformat(timespec="milliseconds")`, then replace `+00:00` with `Z`. |
| `normalize_ts(rfc3339)` | Ingest stores `latest.ts` with Go's `RFC3339Nano`: up to 9 fractional digits, trailing zeros trimmed, maybe no fraction at all (`…:11Z`, `…:11.25Z`, `…:11.250000001Z`). Return the same instant in `iso_ms` format. Python's `fromisoformat` only takes up to 6 fractional digits, so cut the fraction to at most 6 before parsing. |
| `version(device_ids)` | `hashlib.sha1("\n".join(ids).encode()).hexdigest()[:8]`. The separator matters: without it, `["ab","c"]` and `["a","bc"]` hash the same. |
| `states_string(devices, states)` | One character per device, in `devices` order: `STATE_CODES[states[d.id][0]]`, or `-` when the device has no entry. `"".join(...)` over 10,000 devices is fast; don't build the string with `+=`. |
| `machine_json(device, live)` | The machine object from `docs/api.md`: `id`, `name`, `kind`, `state` (`live.state`, `"none"` if empty), `since` (`iso_ms` or `None`), `online`, `last_seen` (`last_seen_s * 1000` through `iso_ms`, or `None`), `latest` (`None` if the hash is empty; otherwise every field except `ts`, `boot`, `seq` as a `float`, plus `ts` through `normalize_ts`). |

**Tests** (`tests/test_model.py`): every `kind` case; `is_online` for all six states; `iso_ms` of a
known instant; `normalize_ts` with 0, 2, 3 and 9 fractional digits; `version` changes when an ID is
added and when two IDs swap order; `states_string` with a missing device; `machine_json` for a device
with no live data (all `None`, state `none`) and for one with full data (`boot`/`seq` not in `latest`).

## Step 2: `app/cache.py`

`TTLCache.get(key, ttl, load)` returns the cached value for `key` if it's younger than `ttl` seconds,
otherwise awaits `load()`, stores the result with the current time, and returns it.

The point of the cache is that 1,000 viewers polling every 5 s cause **one** Redis read per 2 s, not
200 per second. So two things matter:

1. **One loader per key at a time (single-flight).** If 50 requests arrive together on an expired
   key, only the first should call `load()`; the other 49 wait and reuse its result. Get the key's
   lock with `self._locks.setdefault(key, asyncio.Lock())`, `async with` it, and **check the entry
   again inside the lock**: by the time a waiter gets the lock, the first request has already
   refreshed it.
2. **Never cache a failure.** If `load()` raises, let the exception propagate and store nothing, so
   the next request tries again.

Use `self._clock()` for time, never `time.monotonic()` directly: tests pass a fake clock so they don't
sleep. (Monotonic, not wall time: a clock adjustment can't make entries live forever or expire early.)

Interview point: this is the "thundering herd" or cache-stampede problem, and single-flight is the
standard fix.

**Tests** (`tests/test_cache.py`): a second `get` within the TTL doesn't call `load`; after advancing
the fake clock past the TTL it does; 50 concurrent `get`s on one key (`asyncio.gather`, with a `load`
that `await asyncio.sleep(0)` and counts calls) call `load` once; a `load` that raises is retried on
the next `get`; different keys don't share entries.

## Step 3: config, stores, errors, health, main

After this step `/health` works locally and in Docker.

### `app/config.py`

| Function | Write |
|---|---|
| `load()` | Read `DATABASE_URL` and `REDIS_URL` from `os.environ`. If either is missing, raise `RuntimeError` naming **all** the missing ones (same behaviour as ingest's `config.Load`). |

### `app/store/postgres.py` (asyncpg)

| Function | Write |
|---|---|
| `connect(url)` | `await asyncpg.create_pool(url, min_size=1, max_size=10)`. |
| `ping(pool)` | `await pool.fetchval("SELECT 1")`. |
| `fleet(pool, fleet_id)` | `None` if no row in `fleets`. Otherwise a `Fleet` with `name` and every device `WHERE fleet_id = $1 AND deleted_at IS NULL ORDER BY id`, and `version` from `model.version` of the IDs. Always `$1` parameters, never f-strings in SQL (injection). |
| `device(pool, device_id)` | One `Device` with `deleted_at IS NULL`, or `None`. |
| `recent_events(pool, device_id, limit)` | `SELECT from_state, to_state, at FROM machine_events WHERE device_id = $1 ORDER BY at DESC, id DESC LIMIT $2`. That `ORDER BY` matches the index `(device_id, at DESC, id DESC)` from migration 0009, so it reads 10 index entries rather than sorting the table. Return `[{"from", "to", "at"}]` with `at` formatted like `iso_ms` (asyncpg gives a `datetime`). |

### `app/store/redis.py` (redis-py's asyncio client)

Key names are in `storage/redis/keys.md`. Write small private helpers like ingest's
(`_fleet_state(fleet)` → `"fleet:{fleet}:state"`) so each name is spelled once.

| Function | Write |
|---|---|
| `connect(url)` | `Redis.from_url(url, decode_responses=True, socket_timeout=0.5, socket_connect_timeout=0.5)`. The short timeouts make a dead Redis a fast 503, not a page that hangs. |
| `ping(r)` | `await r.ping()`. |
| `fleet_states(r, fleet_id)` | `HGETALL fleet:{fleet}:state`. Each value is `"state\|since_ms"`; split on `\|` once → `{id: (state, int(since_ms))}`. One round trip for the whole fleet. |
| `fleet_counts(r, fleet_id)` | `HGETALL fleet:{fleet}:counts` → `{state: int(n)}`. |
| `rates(r, fleet_id, kind, now_s)` | The last 60 **complete** seconds: `now_s - 60` up to `now_s - 1`. Skip the current second, it's still filling and would always look low. One `MGET` of the 60 keys `fleet:{fleet}:{kind}:{sec}`; a missing key (`None`) is 0. Oldest first. |
| `live(r, device_ids)` | One **pipeline** (`r.pipeline(transaction=False)`) with, per device, `HGETALL device:{id}:state`, `HGETALL device:{id}:latest`, `GET device:{id}:last_seen`. Then `await pipe.execute()` once and turn every three results into a `Live`. 500 devices = 1 round trip, not 1,500. |

### `app/routes/errors.py`

| Name | Write |
|---|---|
| `UNAVAILABLE` | The exceptions that mean "a database is unreachable": `OSError` (refused connections; also covers `TimeoutError`), `asyncpg.PostgresConnectionError`, `asyncpg.InterfaceError`, `redis.exceptions.RedisError`. |
| `fleet_not_found(fleet_id)` | `ApiError(404, "fleet_not_found", f"No fleet with ID '{fleet_id}'.")`. |
| `machine_not_found(device_id)` | The same for machines. |
| `api_error_handler` | `JSONResponse({"error": exc.error, "message": exc.message}, status_code=exc.status)`. |
| `validation_error_handler` | FastAPI answers bad query parameters with 422 and its own shape. The contract says 400 `invalid_query`, so convert: message from the first item of `exc.errors()` (its `loc` and `msg`). |
| `unavailable_handler` | 503 `unavailable`. Log the exception (`logging.exception`) because the client message hides the cause. |

### `app/routes/health.py`

| Function | Write |
|---|---|
| `health` | Try `postgres.ping` and `redis.ping` **separately**, each in its own `try/except UNAVAILABLE`, so a dead Redis still reports Postgres as ok. Body as in `docs/api.md`; status 200 only if both are ok, else 503. Not cached: uptime checks must see the truth. |

### `app/main.py`

| Function | Write |
|---|---|
| `lifespan(app)` | Before `yield`: `settings = config.load()`, then put `postgres.connect(...)`, `redis.connect(...)` and a `TTLCache()` on `app.state` (`app.state.pg`, `app.state.redis`, `app.state.cache`). After `yield`: `await pool.close()` and `await r.aclose()`. Remove the `raise`. Handlers then reach them through `request.app.state`. |
| `create_app()` | `FastAPI(lifespan=lifespan)`; `include_router` for health, fleets, machines; `add_exception_handler` for `ApiError`, `RequestValidationError`, and each class in `UNAVAILABLE`. No gzip middleware: Caddy compresses. |

**Tests** (`tests/conftest.py` + `tests/test_health.py`): a fixture that sets `DATABASE_URL` and
`REDIS_URL` from the `TEST_*` variables (skip the test if they're unset, like the Go tests), runs
`flushdb` on Redis database 15, and yields an `httpx.AsyncClient(transport=httpx.ASGITransport(app=app))`
inside `app.router.lifespan_context(app)`. Test: 200 with both up; 503 with `"redis": "down"` when
`REDIS_URL` points at a closed port (e.g. `redis://localhost:1/0`).

## Step 4: `/devices` and `/states` (`app/routes/fleets.py`)

| Function | Write |
|---|---|
| `load_fleet(request, fleet_id)` | `cache.get(f"fleet:{fleet_id}", 60, lambda: postgres.fleet(pg, fleet_id))`. If the result is `None`, `raise fleet_not_found(fleet_id)`. Every fleet endpoint calls this first, so an unknown fleet is always 404. (A `None` gets cached for 60 s too, which also protects Postgres from someone hammering a bad ID.) |
| `load_states(request, fleet_id)` | `cache.get(f"states:{fleet_id}", 2, lambda: redis.fleet_states(r, fleet_id))`. |
| `devices` | `fleet = await load_fleet(...)`; return `{"fleet", "version", "devices": [{"id", "name", "kind"}]}`. Set `response.headers["Cache-Control"] = "public, max-age=60"`. |
| `states` | `fleet` and `states` loaded as above; return `{"fleet", "version", "states": model.states_string(fleet.devices, states)}`. `max-age=2`. The version and the string come from the same `fleet.devices` list, so they always line up. |

**Tests** (`tests/test_fleets.py`): insert a fleet and 3 devices (one soft-deleted) into the test
database and write `fleet:{id}:state` entries into Redis database 15 by hand, exactly as ingest would.
Check ordering by ID, the deleted device missing, `kind` mapping, a device with no Redis entry as `-`,
404 for an unknown fleet, and that `version` in both responses matches.

## Step 5: `/summary`

| Function | Write |
|---|---|
| `summary` | Cache the whole response dict for 2 s under `summary:{fleet_id}`. Inside the loader: `load_fleet`, `fleet_counts`, `rates` for `readings` and `transitions` with `now_s = int(time.time())` (wall time here: the keys are Unix seconds). `devices = len(fleet.devices)`; `counts` has all six states, missing ones are 0; `none = max(0, devices − sum(the other five))`. The `max(0, …)` covers a device deleted in Postgres whose Redis entry hasn't been rebuilt yet. `online` = sum of the four online states. `generated_at` = `iso_ms` of now. `max-age=2`. |

Why counts come from `fleet:{id}:counts` instead of tallying `fleet_states`: it's 6 fields instead of
10,000, kept exactly in sync by ingest's Lua script.

**Tests**: counts sum to `devices`; `none` fills the remainder; the rate arrays are 60 long, oldest
first, with a key you set for `now − 1` at the end, and the current second's key ignored.

## Step 6: `/machines` (paged)

| Function | Write |
|---|---|
| `machines` | 1. `fleet = load_fleet(...)`. 2. Candidates: devices with `id > after` (if `after` is set). The list is already sorted, so use `bisect` on the IDs or a simple filter. 3. If `state` is set, filter with `load_states` (missing entry = `"none"`). 4. Take `limit + 1`: if you got the extra one, there's a next page, and `next` = the ID of the last one **returned** (not the extra). 5. `redis.live` for just those ≤ 500 IDs and `model.machine_json` each. Cache the final dict 2 s under a key with all the query values (`machines:{fleet}:{state}:{limit}:{after}`). `max-age=2`. |

`state` and `limit` are already validated by their type annotations (`State`, `Query(ge=1, le=500)`);
a bad value raises `RequestValidationError`, which your handler turns into 400.

Interview point: keyset pagination (`after=<last id>`) instead of `offset`: a page is stable while devices
are added, and it costs the same on page 1 and page 100.

**Tests**: two pages with `limit=2` cover all devices exactly once and the last page has `next: null`;
`state=running` returns only running ones; `state=none` returns devices without Redis state;
`limit=0`, `limit=501` and `state=bogus` give 400 `invalid_query`.

## Step 7: `/machines/{id}` (`app/routes/machines.py`)

| Function | Write |
|---|---|
| `machine` | Cache 2 s under `machine:{device_id}`. `postgres.device` (`None` → `machine_not_found`), `redis.live([device_id])[0]`, `postgres.recent_events`. Return `machine_json(...)` plus `"fleet": device.fleet_id` and `"events"`. `max-age=2`. |

**Tests**: 404 for unknown and for soft-deleted; `events` newest first and at most 10 (insert 12).

## Step 8: run it on the server

Add the service to `deploy/docker-compose.yml`:

```yaml
  api:
    build:
      context: ../services/api
    restart: unless-stopped
    environment:
      DATABASE_URL: postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@postgres:5432/${POSTGRES_DB}
      REDIS_URL: redis://redis:6379/0
    depends_on:
      postgres:
        condition: service_healthy
      redis:
        condition: service_healthy
```

Then `deploy/deploy.sh` and `curl -s https://spincycle.alyfarghali.com/api/fleets/laundry/summary`.
SpinCycle's Caddy already proxies `/api/*` and `/health` to `api:8000`.
