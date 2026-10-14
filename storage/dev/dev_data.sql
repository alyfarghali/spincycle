-- Development-only test data: the laundry fleet and test devices. Never loaded in production.
-- Load by hand after a fresh database (see storage/dev/README.md). Safe to re-run.

INSERT INTO fleets (id, name) VALUES
    ('laundry', 'Laundry room')
ON CONFLICT (id) DO NOTHING;

INSERT INTO devices (id, fleet_id, type_id, display_name) VALUES
    ('c6-washer-03', 'laundry', 'laundry-washer', 'Washer 3')
ON CONFLICT (id) DO NOTHING;
