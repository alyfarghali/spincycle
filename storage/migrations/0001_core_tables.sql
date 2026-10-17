-- Tables: fleets, device_types, devices (with soft delete), machine_events.
<<<<<<< HEAD
CREATE TABLE fleets (
    id TEXT PRIMARY KEY CHECK (id ~ '^[a-z0-9][a-z0-9-]{0,31}$'),
    name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE device_types (
    id TEXT PRIMARY KEY CHECK (id ~ '^[a-z0-9][a-z0-9-]{0,31}$'),
    metrics JSONB NOT NULL,
    config JSONB NOT NULL DEFAULT '{}'
);

CREATE TABLE devices (
    id TEXT PRIMARY KEY CHECK (id ~ '^[a-z0-9][a-z0-9-]{0,31}$'),
    fleet_id TEXT NOT NULL REFERENCES fleets(id),
    type_id TEXT NOT NULL REFERENCES device_types(id),
    display_name TEXT NOT NULL,
    config JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ
);
=======
>>>>>>> main
