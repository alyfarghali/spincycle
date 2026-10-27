package dedup

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/alyfarghali/spincycle/services/ingest/decoder"
	"github.com/alyfarghali/spincycle/services/ingest/engine"
)

var t0 = time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)

func setup(t *testing.T) (*Store, *pgxpool.Pool, string) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; run storage/test-db.sh")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	device := fmt.Sprintf("storetest-%d", time.Now().UnixNano()%1e9)
	for _, sql := range []string{
		`INSERT INTO fleets (id, name) VALUES ('storetest', 'store test') ON CONFLICT DO NOTHING`,
		`INSERT INTO devices (id, fleet_id, type_id, display_name) VALUES ('` + device + `', 'storetest', 'laundry-washer', 'x')`,
	} {
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	return NewStore(pool), pool, device
}

func count(t *testing.T, pool *pgxpool.Pool, table, device string) int {
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM `+table+` WHERE device_id = $1`, device).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func reading(device string, seq uint64) decoder.Reading {
	return decoder.Reading{DeviceID: device, Boot: 1, Seq: seq, Time: t0.Add(time.Duration(seq) * 5 * time.Second),
		Metrics: map[string]float64{"vibration_rms": 0.03}}
}

func TestSaveStoresReadingAndTransitionsTogether(t *testing.T) {
	s, pool, dev := setup(t)
	ctx := context.Background()
	tr := []engine.Transition{{From: engine.Free, To: engine.Running, At: t0}}

	stored, err := s.Save(ctx, reading(dev, 0), tr)
	if err != nil || !stored {
		t.Fatalf("stored %v, err %v", stored, err)
	}
	if count(t, pool, "telemetry", dev) != 1 || count(t, pool, "readings_seen", dev) != 1 || count(t, pool, "machine_events", dev) != 1 {
		t.Error("reading, dedup mark and transition should all be written")
	}
}

func TestDuplicateWritesNoTransitions(t *testing.T) {
	s, pool, dev := setup(t)
	ctx := context.Background()
	tr := []engine.Transition{{From: engine.Free, To: engine.Running, At: t0}}

	s.Save(ctx, reading(dev, 0), tr)
	stored, err := s.Save(ctx, reading(dev, 0), tr) // contract A8: redelivery
	if err != nil || stored {
		t.Fatalf("a redelivered reading must be a duplicate: stored %v, err %v", stored, err)
	}
	if count(t, pool, "machine_events", dev) != 1 || count(t, pool, "telemetry", dev) != 1 {
		t.Error("a duplicate must write nothing")
	}
}

func TestFailedTransitionRollsBackTheReading(t *testing.T) {
	s, pool, dev := setup(t)
	bad := []engine.Transition{{From: engine.Running, To: engine.Running, At: t0}} // violates from_state <> to_state

	if _, err := s.Save(context.Background(), reading(dev, 0), bad); err == nil {
		t.Fatal("expected an error")
	}
	if count(t, pool, "telemetry", dev) != 0 || count(t, pool, "readings_seen", dev) != 0 {
		t.Error("the reading must roll back with its transition, so redelivery retries both")
	}
}

func TestSaveTransitions(t *testing.T) {
	s, pool, dev := setup(t)
	err := s.SaveTransitions(context.Background(), dev, []engine.Transition{
		{From: engine.Running, To: engine.Offline, At: t0},
		{From: engine.Offline, To: engine.Running, At: t0.Add(time.Minute)},
	})
	if err != nil || count(t, pool, "machine_events", dev) != 2 {
		t.Errorf("err %v, rows %d", err, count(t, pool, "machine_events", dev))
	}
}

func TestLoadStatesRestoresLatestTransition(t *testing.T) {
	s, _, dev := setup(t)
	_, _, offlineDev := setup(t)
	ctx := context.Background()
	s.SaveTransitions(ctx, dev, []engine.Transition{
		{From: engine.Unknown, To: engine.Free, At: t0},
		{From: engine.Free, To: engine.Running, At: t0.Add(time.Minute)},
		{From: engine.Running, To: engine.Finished, At: t0.Add(40 * time.Minute)},
	})
	s.SaveTransitions(ctx, offlineDev, []engine.Transition{
		{From: engine.Free, To: engine.Running, At: t0},
		{From: engine.Running, To: engine.Offline, At: t0.Add(10 * time.Minute)},
	})

	now := t0.Add(time.Hour)
	states, err := s.LoadStates(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if m := states[dev]; m.State != engine.Finished || !m.Since.Equal(t0.Add(40*time.Minute)) || !m.LastReading.Equal(now) {
		t.Errorf("finished device: %+v", m)
	}
	if m := states[offlineDev]; m.State != engine.Offline || m.Prev != engine.Running {
		t.Errorf("offline device must remember it was running, so it can resume: %+v", m)
	}
}
