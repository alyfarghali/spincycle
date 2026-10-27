-- Placeholder thresholds until real recordings exist (testdata/cycles); retune with a new migration.
UPDATE device_types
SET config = config || '{"on_threshold": 0.01, "off_threshold": 0.005, "start_confirm": 30, "stop_confirm": 300,
                         "clear_after": 1200, "offline_after": 60, "resume_window": 600}'
WHERE id IN ('laundry-washer', 'laundry-dryer', 'loadtest-sim');
