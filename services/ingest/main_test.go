package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/eclipse/paho.golang/paho"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/alyfarghali/spincycle/services/ingest/dedup"
	"github.com/alyfarghali/spincycle/services/ingest/engine"
	"github.com/alyfarghali/spincycle/services/ingest/livestate"
	"github.com/alyfarghali/spincycle/services/ingest/metrics"
	"github.com/alyfarghali/spincycle/services/ingest/registry"
	"github.com/alyfarghali/spincycle/services/ingest/subscriber"
)

var params = engine.Params{OnThreshold: 0.01, OffThreshold: 0.005, StartConfirm: 30 * time.Second,
	StopConfirm: 5 * time.Minute, ClearAfter: 20 * time.Minute, OfflineAfter: time.Minute, ResumeWindow: 10 * time.Minute}

type harness struct {
	t     *testing.T
	w     *worker
	pool  *pgxpool.Pool
	dev   string
	base  time.Time // inside the decoder's ±1 h window
	acked int
	seq   uint64
}

func newHarness(t *testing.T, engineErr error) *harness {
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
	h := &harness{t: t, pool: pool, dev: fmt.Sprintf("wtest-%d", time.Now().UnixNano()%1e9),
		base: time.Now().UTC().Add(-30 * time.Minute).Truncate(time.Second)}
	for _, sql := range []string{
		`INSERT INTO fleets (id, name) VALUES ('wtest', 'worker test') ON CONFLICT DO NOTHING`,
		`INSERT INTO devices (id, fleet_id, type_id, display_name) VALUES ('` + h.dev + `', 'wtest', 'laundry-washer', 'x')`,
	} {
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	reg := registry.New(registry.Device{ID: h.dev, FleetID: "wtest", TypeID: "laundry-washer",
		Metrics: map[string]bool{"vibration_rms": true}, Engine: params, EngineErr: engineErr})
	h.w = &worker{reg: reg, store: dedup.NewStore(pool), counts: metrics.New(), machines: map[string]engine.Machine{},
		ack: func(subscriber.Message) error { h.acked++; return nil }}
	return h
}

func (h *harness) publish(kind string, payload []byte) {
	topic := fmt.Sprintf("fleets/wtest/devices/%s/%s", h.dev, kind)
	h.w.handle(context.Background(), subscriber.Message{Packet: &paho.Publish{Topic: topic, Payload: payload}})
}

func (h *harness) reading(at time.Duration, rms float64) []byte {
	body, _ := json.Marshal(map[string]any{"v": 1, "device_id": h.dev, "fw": "test", "boot": 1, "seq": h.seq,
		"ts": h.base.Add(at).Format("2006-01-02T15:04:05.000Z"), "metrics": map[string]float64{"vibration_rms": rms}})
	h.seq++
	return body
}

func (h *harness) readings(from, to time.Duration, rms float64) {
	for at := from; at < to; at += 5 * time.Second {
		h.publish("telemetry", h.reading(at, rms))
	}
}

func (h *harness) events() []string {
	rows, err := h.pool.Query(context.Background(),
		`SELECT from_state || '>' || to_state FROM machine_events WHERE device_id = $1 ORDER BY id`, h.dev)
	if err != nil {
		h.t.Fatal(err)
	}
	var out []string
	for rows.Next() {
		var e string
		rows.Scan(&e)
		out = append(out, e)
	}
	return out
}

func (h *harness) expect(state engine.State, events ...string) {
	h.t.Helper()
	if got := h.events(); !slices.Equal(got, events) {
		h.t.Errorf("machine_events %v, want %v", got, events)
	}
	if got := h.w.machines[h.dev].State; got != state {
		h.t.Errorf("state in memory %q, want %q", got, state)
	}
}

func TestReadingsDriveTheEngine(t *testing.T) {
	h := newHarness(t, nil)
	h.readings(0, 60*time.Second, 0.0015)
	h.readings(60*time.Second, 2*time.Minute, 0.03)
	h.expect(engine.Running, "unknown>free", "free>running")
	if h.acked != 24 {
		t.Errorf("acked %d of 24 messages", h.acked)
	}
}

func TestRedeliveryAddsNoTransition(t *testing.T) {
	h := newHarness(t, nil)
	h.readings(0, 35*time.Second, 0.0015) // the reading at 30 s completes start_confirm: unknown>free
	h.seq--                               // contract A8: the broker redelivers that reading
	h.publish("telemetry", h.reading(30*time.Second, 0.0015))
	h.expect(engine.Free, "unknown>free")
	if h.w.counts.Snapshot()["duplicate"] != 1 {
		t.Error("redelivery should count as a duplicate")
	}
}

func TestLastWillThenShortDropResumes(t *testing.T) {
	h := newHarness(t, nil)
	h.readings(0, 60*time.Second, 0.0015)
	h.readings(60*time.Second, 2*time.Minute, 0.03)
	h.publish("status", []byte("offline"))
	h.publish("status", []byte("online"))
	h.readings(4*time.Minute, 5*time.Minute, 0.03)
	h.expect(engine.Running, "unknown>free", "free>running", "running>offline", "offline>running")
}

func TestTickMarksSilentDeviceOffline(t *testing.T) {
	h := newHarness(t, nil)
	h.readings(0, 60*time.Second, 0.0015)
	h.w.tick(context.Background(), h.base.Add(55*time.Second+params.OfflineAfter))
	h.expect(engine.Offline, "unknown>free", "free>offline")
	h.w.tick(context.Background(), h.base.Add(10*time.Minute))
	h.expect(engine.Offline, "unknown>free", "free>offline")
}

func TestBadStatusIsAckedAndCounted(t *testing.T) {
	h := newHarness(t, nil)
	h.readings(0, 60*time.Second, 0.0015)
	h.publish("status", []byte("offline\n"))
	h.expect(engine.Free, "unknown>free")
	if h.acked != 13 || h.w.counts.Snapshot()["bad_status"] != 1 {
		t.Errorf("acked %d, counts %v", h.acked, h.w.counts.Snapshot())
	}
}

func TestBrokenConfigStoresReadingsWithoutState(t *testing.T) {
	h := newHarness(t, fmt.Errorf("broken"))
	h.readings(0, 2*time.Minute, 0.03)
	h.expect("")
	var n int
	h.pool.QueryRow(context.Background(), `SELECT count(*) FROM telemetry WHERE device_id = $1`, h.dev).Scan(&n)
	if n != 24 {
		t.Errorf("stored %d of 24 readings", n)
	}
}

func TestRestartRestoresState(t *testing.T) {
	h := newHarness(t, nil)
	h.readings(0, 60*time.Second, 0.0015)
	h.readings(60*time.Second, 2*time.Minute, 0.03)
	restored, err := h.w.store.LoadStates(context.Background(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if m := restored[h.dev]; m.State != engine.Running || !m.Since.Equal(h.w.machines[h.dev].Since) {
		t.Errorf("restored %+v, in memory %+v", m, h.w.machines[h.dev])
	}
}

func redisFor(t *testing.T, url string) *redis.Client {
	opts, err := redis.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	opts.DialTimeout, opts.ReadTimeout, opts.WriteTimeout, opts.MaxRetries = redisTimeout, redisTimeout, redisTimeout, -1
	rdb := redis.NewClient(opts)
	t.Cleanup(func() { rdb.Close() })
	return rdb
}

func testRedis(t *testing.T) *redis.Client {
	url := os.Getenv("TEST_REDIS_URL")
	if url == "" {
		t.Skip("TEST_REDIS_URL not set, e.g. redis://localhost:6379/15")
	}
	rdb := redisFor(t, url)
	if rdb.Options().DB == 0 {
		t.Fatal("TEST_REDIS_URL must not use database 0: tests flush it")
	}
	rdb.FlushDB(context.Background())
	rdb.ScriptFlush(context.Background()) // like a restarted Redis: no cached scripts
	return rdb
}

func TestLiveStateFollowsReadings(t *testing.T) {
	h := newHarness(t, nil)
	rdb := testRedis(t)
	h.w.live = livestate.New(rdb)
	h.readings(0, 60*time.Second, 0.0015)
	h.readings(60*time.Second, 2*time.Minute, 0.03)

	ctx := context.Background()
	if got := rdb.HGetAll(ctx, livestate.FleetCounts("wtest")).Val(); got["running"] != "1" || got["free"] != "0" {
		t.Errorf("fleet counts %v, want the device moved from free to running", got)
	}
	if rdb.HGet(ctx, livestate.DeviceState(h.dev), "state").Val() != "running" {
		t.Error("device state not written")
	}
	if rdb.HGet(ctx, livestate.DeviceLatest(h.dev), "seq").Val() != "23" {
		t.Error("latest reading not written")
	}
}

func TestRedisDownNeverBlocksStorage(t *testing.T) {
	h := newHarness(t, nil)
	h.w.live = livestate.New(redisFor(t, "redis://127.0.0.1:1/15")) // nothing listens on port 1
	start := time.Now()
	h.readings(0, 2*time.Minute, 0.03)
	elapsed := time.Since(start)

	var stored int
	h.pool.QueryRow(context.Background(), `SELECT count(*) FROM telemetry WHERE device_id = $1`, h.dev).Scan(&stored)
	c := h.w.counts.Snapshot()
	if stored != 24 || h.acked != 24 {
		t.Errorf("stored %d, acked %d of 24", stored, h.acked)
	}
	if c["redis_error"] != 1 || c["redis_skipped"] != 23 {
		t.Errorf("want 1 failed write then 23 skipped during the backoff, got %v", c)
	}
	if elapsed > 2*time.Second {
		t.Errorf("24 readings took %v: a dead Redis must cost at most one timeout", elapsed)
	}
	if !h.w.liveDirty {
		t.Error("missed writes must mark the cache for a rebuild")
	}
}

func TestTickRebuildsCacheAfterOutage(t *testing.T) {
	h := newHarness(t, nil)
	rdb := testRedis(t)
	h.readings(0, 2*time.Minute, 0.03) // live is nil: nothing reaches Redis, like an outage
	h.w.live = livestate.New(rdb)
	h.w.liveDirty = true

	h.w.tick(context.Background(), h.base.Add(2*time.Minute))
	if rdb.HGet(context.Background(), livestate.FleetCounts("wtest"), "running").Val() != "1" || h.w.liveDirty {
		t.Error("the tick should rebuild the fleet from memory and clear the flag")
	}
}
