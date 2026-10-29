package livestate

import (
	"context"
	"encoding/json"
	"math/rand/v2"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/alyfarghali/spincycle/services/ingest/decoder"
	"github.com/alyfarghali/spincycle/services/ingest/engine"
)

var t0 = time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)

func testRedis(t *testing.T) *redis.Client {
	url := os.Getenv("TEST_REDIS_URL")
	if url == "" {
		t.Skip("TEST_REDIS_URL not set, e.g. redis://localhost:6379/15")
	}
	opts, err := redis.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	if opts.DB == 0 {
		t.Fatal("TEST_REDIS_URL must not use database 0: tests flush it")
	}
	rdb := redis.NewClient(opts)
	t.Cleanup(func() { rdb.Close() })
	if err := rdb.FlushDB(context.Background()).Err(); err != nil {
		t.Fatal(err)
	}
	rdb.ScriptFlush(context.Background()) // like a restarted Redis: no cached scripts
	return rdb
}

func machine(s engine.State, since time.Time) engine.Machine {
	return engine.Machine{State: s, Since: since}
}

func TestReadingWritesLatestStateAndRate(t *testing.T) {
	rdb := testRedis(t)
	ctx := context.Background()
	r := decoder.Reading{DeviceID: "d1", FleetID: "f", Boot: 3, Seq: 42, Time: t0.Add(250 * time.Millisecond),
		Received: t0.Add(time.Second), Metrics: map[string]float64{"vibration_rms": 0.0123}}

	if err := New(rdb).Reading(ctx, "f", r, machine(engine.Free, t0)); err != nil {
		t.Fatal(err)
	}
	latest := rdb.HGetAll(ctx, DeviceLatest("d1")).Val()
	if latest["ts"] != "2026-10-27T10:00:00.25Z" || latest["seq"] != "42" || latest["boot"] != "3" || latest["vibration_rms"] != "0.0123" {
		t.Errorf("latest %v", latest)
	}
	if got := rdb.Get(ctx, DeviceLastSeen("d1")).Val(); got != strconv.FormatInt(t0.Add(time.Second).Unix(), 10) {
		t.Errorf("last_seen %q", got)
	}
	if got := rdb.HGet(ctx, FleetState("f"), "d1").Val(); got != "free|"+strconv.FormatInt(t0.UnixMilli(), 10) {
		t.Errorf("fleet state %q", got)
	}
	if got := rdb.HGetAll(ctx, FleetCounts("f")).Val(); got["free"] != "1" || len(got) != 1 {
		t.Errorf("counts %v", got)
	}
	rate := FleetReadings("f", t0.Add(time.Second).Unix())
	if rdb.Get(ctx, rate).Val() != "1" || rdb.TTL(ctx, rate).Val() <= 0 {
		t.Error("per-second reading counter should be 1 and expire")
	}
}

func TestDeviceWithoutStateIsNotCounted(t *testing.T) {
	rdb := testRedis(t)
	ctx := context.Background()
	r := decoder.Reading{DeviceID: "d1", Received: t0, Metrics: map[string]float64{"vibration_rms": 0.01}}
	New(rdb).Reading(ctx, "f", r, engine.Machine{})
	if rdb.Exists(ctx, FleetState("f"), FleetCounts("f")).Val() != 0 {
		t.Error("a device with no engine state must not appear in the fleet map or counts")
	}
}

// Whatever order updates arrive in, the counts always equal a tally of the fleet map.
func TestCountsAlwaysMatchTheFleetMap(t *testing.T) {
	rdb := testRedis(t)
	ctx := context.Background()
	w := New(rdb)
	states := []engine.State{engine.Free, engine.Running, engine.Finished, engine.Offline, engine.Unknown}
	rng := rand.New(rand.NewPCG(1, 2))
	for i := range 2000 {
		id := "d" + strconv.Itoa(rng.IntN(50))
		m := machine(states[rng.IntN(len(states))], t0.Add(time.Duration(rng.IntN(100))*time.Second))
		tr := []engine.Transition{{From: engine.Unknown, To: m.State, At: m.Since}}
		if err := w.Transitions(ctx, "f", id, m, tr, t0.Add(time.Duration(i)*time.Millisecond)); err != nil {
			t.Fatal(err)
		}
	}
	tally := map[string]int{}
	for _, v := range rdb.HGetAll(ctx, FleetState("f")).Val() {
		for i := range v {
			if v[i] == '|' {
				tally[v[:i]]++
				break
			}
		}
	}
	for s, n := range rdb.HGetAll(ctx, FleetCounts("f")).Val() {
		if c, _ := strconv.Atoi(n); c != tally[s] {
			t.Errorf("count[%s] = %d, map has %d", s, c, tally[s])
		}
		delete(tally, s)
	}
	for s, n := range tally {
		if n != 0 {
			t.Errorf("state %s in the map (%d) but missing from counts", s, n)
		}
	}
}

func TestTransitionsPublishEvents(t *testing.T) {
	rdb := testRedis(t)
	ctx := context.Background()
	sub := rdb.Subscribe(ctx, EventsChannel)
	defer sub.Close()
	if _, err := sub.Receive(ctx); err != nil {
		t.Fatal(err)
	}

	tr := []engine.Transition{
		{From: engine.Free, To: engine.Running, At: t0},
		{From: engine.Running, To: engine.Offline, At: t0.Add(time.Minute)},
	}
	if err := New(rdb).Transitions(ctx, "f", "d1", machine(engine.Offline, t0.Add(time.Minute)), tr, t0); err != nil {
		t.Fatal(err)
	}
	for _, want := range tr {
		msg, err := sub.ReceiveTimeout(ctx, 2*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		var e Event
		json.Unmarshal([]byte(msg.(*redis.Message).Payload), &e)
		if e.DeviceID != "d1" || e.FleetID != "f" || e.From != want.From || e.To != want.To || !e.At.Equal(want.At) {
			t.Errorf("event %+v, want %+v", e, want)
		}
	}
	if rdb.Get(ctx, FleetTransitions("f", t0.Unix())).Val() != "2" {
		t.Error("per-second transition counter should be 2")
	}
}

func TestRebuildReplacesStaleEntries(t *testing.T) {
	rdb := testRedis(t)
	ctx := context.Background()
	rdb.HSet(ctx, FleetState("f"), "gone", "running|1")
	rdb.HSet(ctx, FleetCounts("f"), "running", 99)

	states := map[string]engine.Machine{
		"a": machine(engine.Free, t0), "b": machine(engine.Running, t0), "c": machine(engine.Running, t0),
		"other-fleet": machine(engine.Finished, t0), "unregistered": machine(engine.Free, t0), "no-state": {},
	}
	fleetOf := func(id string) (string, bool) {
		switch id {
		case "other-fleet":
			return "g", true
		case "unregistered":
			return "", false
		}
		return "f", true
	}
	if err := New(rdb).Rebuild(ctx, states, fleetOf); err != nil {
		t.Fatal(err)
	}
	if got := rdb.HKeys(ctx, FleetState("f")).Val(); len(got) != 3 {
		t.Errorf("fleet f devices %v, want a, b, c only", got)
	}
	if got := rdb.HGetAll(ctx, FleetCounts("f")).Val(); got["free"] != "1" || got["running"] != "2" || len(got) != 2 {
		t.Errorf("counts %v", got)
	}
	if rdb.HGet(ctx, FleetCounts("g"), "finished").Val() != "1" {
		t.Error("each fleet is rebuilt separately")
	}
	if rdb.HGet(ctx, DeviceState("b"), "state").Val() != "running" {
		t.Error("device state hashes are rewritten too")
	}
}
