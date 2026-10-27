INSERT INTO fleets (id, name) VALUES ('loadtest', 'Load test')
ON CONFLICT (id) DO NOTHING;

INSERT INTO devices (id, fleet_id, type_id, display_name)
SELECT 'loadtest-' || lpad(i::text, 5, '0'), 'loadtest', 'loadtest-sim', 'Sim ' || i
FROM generate_series(1, 10000) AS i
ON CONFLICT (id) DO NOTHING;
