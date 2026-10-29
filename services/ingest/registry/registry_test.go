package registry

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func testPool(t *testing.T) *pgxpool.Pool {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; run storage/test-db.sh")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestLoadParsesEngineParams(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO fleets (id, name) VALUES ('regtest', 'registry test') ON CONFLICT DO NOTHING`)
	exec(`INSERT INTO devices (id, fleet_id, type_id, display_name, config) VALUES
		('regtest-plain', 'regtest', 'laundry-washer', 'plain', '{}'),
		('regtest-override', 'regtest', 'laundry-washer', 'override', '{"stop_confirm": 420, "interval_s": 5}'),
		('regtest-broken', 'regtest', 'laundry-washer', 'broken', '{"off_threshold": 0.5}'),
		('regtest-deleted', 'regtest', 'laundry-washer', 'deleted', '{}')
		ON CONFLICT (id) DO UPDATE SET config = EXCLUDED.config`)
	exec(`UPDATE devices SET deleted_at = now() WHERE id = 'regtest-deleted'`)

	reg, err := Load(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}

	plain, ok := reg.Lookup("regtest-plain")
	if !ok || plain.EngineErr != nil || plain.Engine.StopConfirm != 5*time.Minute || !plain.Metrics["vibration_rms"] {
		t.Errorf("plain: %+v", plain)
	}
	if o, _ := reg.Lookup("regtest-override"); o.EngineErr != nil || o.Engine.StopConfirm != 7*time.Minute {
		t.Errorf("devices.config should override the type: %+v", o)
	}
	if b, ok := reg.Lookup("regtest-broken"); !ok || b.EngineErr == nil {
		t.Errorf("a broken config must be reported but the device kept: %+v", b)
	}
	if _, ok := reg.Lookup("regtest-deleted"); ok {
		t.Error("soft-deleted devices must not be loaded")
	}
}
