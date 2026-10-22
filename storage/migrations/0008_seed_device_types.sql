INSERT INTO device_types (id, metrics, config) VALUES
    ('laundry-washer', '["vibration_rms"]', '{}'),
    ('laundry-dryer',  '["vibration_rms"]', '{}'),
    ('loadtest-sim',   '["vibration_rms"]', '{}')
ON CONFLICT (id) DO NOTHING;
