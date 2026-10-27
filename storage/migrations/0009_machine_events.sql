CREATE TABLE machine_events (
    id BIGSERIAL PRIMARY KEY,
    device_id TEXT NOT NULL REFERENCES devices(id),
    from_state TEXT NOT NULL CHECK (from_state IN ('free', 'running', 'finished', 'offline', 'unknown')),
    to_state TEXT NOT NULL CHECK (to_state IN ('free', 'running', 'finished', 'offline', 'unknown')),
    at TIMESTAMPTZ NOT NULL,
    CHECK (from_state <> to_state)
);

CREATE INDEX machine_events_device_at ON machine_events (device_id, at DESC, id DESC);
