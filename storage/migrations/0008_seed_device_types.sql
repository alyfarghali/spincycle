-- Default washer and dryer device types with starting thresholds and timers.
-- Thresholds and timers stay empty until real cycles are recorded (contract Part C).
-- Safe to re-run: existing types are left unchanged.

INSERT INTO device_types (id, metrics, config) VALUES
    ('laundry-washer', '["vibration_rms"]', '{}'),
    ('laundry-dryer',  '["vibration_rms"]', '{}'),
    ('loadtest-sim',   '["vibration_rms"]', '{}')
ON CONFLICT (id) DO NOTHING;
