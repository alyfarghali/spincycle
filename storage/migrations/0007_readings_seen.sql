CREATE TABLE readings_seen (
    device_id TEXT NOT NULL,
    boot BIGINT NOT NULL,
    seq BIGINT NOT NULL,
    seen_at TIMESTAMPTZ DEFAULT now(),
    PRIMARY KEY (device_id, boot, seq)
);