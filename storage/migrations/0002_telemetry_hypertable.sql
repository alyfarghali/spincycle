CREATE EXTENSION IF NOT EXISTS timescaledb;

CREATE TABLE telemetry (
    time TIMESTAMPTZ NOT NULL,
    device_id TEXT NOT NULL,
    metric TEXT NOT NULL,
    value DOUBLE PRECISION NOT NULL
);

SELECT create_hypertable('telemetry', by_range('time', INTERVAL '1 day'));
CREATE INDEX telemetry_device_time_idx ON telemetry (device_id, time DESC);
