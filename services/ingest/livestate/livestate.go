package livestate

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/alyfarghali/spincycle/services/ingest/decoder"
	"github.com/alyfarghali/spincycle/services/ingest/engine"
)

const (
	EventsChannel = "events"
	rateTTL       = 2 * time.Minute
)

type Writer struct {
	rdb *redis.Client
}

func New(rdb *redis.Client) *Writer { return &Writer{rdb: rdb} }

func DeviceLatest(id string) string   { return "device:" + id + ":latest" }
func DeviceLastSeen(id string) string { return "device:" + id + ":last_seen" }
func DeviceState(id string) string    { return "device:" + id + ":state" }
func FleetState(fleet string) string  { return "fleet:" + fleet + ":state" }
func FleetCounts(fleet string) string {
	return "fleet:" + fleet + ":counts"
}
func FleetReadings(fleet string, sec int64) string {
	return "fleet:" + fleet + ":readings:" + strconv.FormatInt(sec, 10)
}
func FleetTransitions(fleet string, sec int64) string {
	return "fleet:" + fleet + ":transitions:" + strconv.FormatInt(sec, 10)
}

// One atomic step, so the counts can't drift from the per-device entries whatever order updates arrive in.
var setState = redis.NewScript(`
local prev = redis.call('HGET', KEYS[1], ARGV[1])
local new = ARGV[2] .. '|' .. ARGV[3]
if prev == new then return 0 end
redis.call('HSET', KEYS[1], ARGV[1], new)
if prev then redis.call('HINCRBY', KEYS[2], string.match(prev, '^[^|]+'), -1) end
redis.call('HINCRBY', KEYS[2], ARGV[2], 1)
return 1
`)

func stateArgs(m engine.Machine) []any {
	return []any{string(m.State), m.Since.UnixMilli()}
}

func queueState(ctx context.Context, p redis.Pipeliner, fleet, id string, m engine.Machine) {
	// Eval, not Run: in a pipeline EVALSHA can't fall back, so a fresh or restarted Redis would answer NOSCRIPT.
	setState.Eval(ctx, p, []string{FleetState(fleet), FleetCounts(fleet)}, append([]any{id}, stateArgs(m)...)...)
	p.HSet(ctx, DeviceState(id), "state", string(m.State), "since", m.Since.UnixMilli())
}

func (w *Writer) Reading(ctx context.Context, fleet string, r decoder.Reading, m engine.Machine) error {
	sec := r.Received.Unix()
	_, err := w.rdb.Pipelined(ctx, func(p redis.Pipeliner) error {
		fields := []any{"ts", r.Time.UTC().Format(time.RFC3339Nano), "boot", r.Boot, "seq", r.Seq}
		for name, v := range r.Metrics {
			fields = append(fields, name, v)
		}
		p.HSet(ctx, DeviceLatest(r.DeviceID), fields...)
		p.Set(ctx, DeviceLastSeen(r.DeviceID), sec, 0)
		p.Incr(ctx, FleetReadings(fleet, sec))
		p.Expire(ctx, FleetReadings(fleet, sec), rateTTL)
		if m.State != "" {
			queueState(ctx, p, fleet, r.DeviceID, m)
		}
		return nil
	})
	return err
}

type Event struct {
	DeviceID string       `json:"device_id"`
	FleetID  string       `json:"fleet_id"`
	From     engine.State `json:"from"`
	To       engine.State `json:"to"`
	At       time.Time    `json:"at"`
}

func (w *Writer) Transitions(ctx context.Context, fleet, id string, m engine.Machine, transitions []engine.Transition, now time.Time) error {
	if len(transitions) == 0 {
		return nil
	}
	_, err := w.rdb.Pipelined(ctx, func(p redis.Pipeliner) error {
		queueState(ctx, p, fleet, id, m)
		for _, t := range transitions {
			body, err := json.Marshal(Event{DeviceID: id, FleetID: fleet, From: t.From, To: t.To, At: t.At.UTC()})
			if err != nil {
				return err
			}
			p.Publish(ctx, EventsChannel, body)
		}
		p.IncrBy(ctx, FleetTransitions(fleet, now.Unix()), int64(len(transitions)))
		p.Expire(ctx, FleetTransitions(fleet, now.Unix()), rateTTL)
		return nil
	})
	return err
}

func (w *Writer) Rebuild(ctx context.Context, states map[string]engine.Machine, fleetOf func(id string) (string, bool)) error {
	byFleet := map[string]map[string]engine.Machine{}
	for id, m := range states {
		if m.State == "" {
			continue
		}
		fleet, ok := fleetOf(id)
		if !ok {
			continue
		}
		if byFleet[fleet] == nil {
			byFleet[fleet] = map[string]engine.Machine{}
		}
		byFleet[fleet][id] = m
	}
	_, err := w.rdb.TxPipelined(ctx, func(p redis.Pipeliner) error {
		for fleet, machines := range byFleet {
			p.Del(ctx, FleetState(fleet), FleetCounts(fleet))
			entries := make([]any, 0, 2*len(machines))
			counts := map[engine.State]int64{}
			for id, m := range machines {
				entries = append(entries, id, fmt.Sprintf("%s|%d", m.State, m.Since.UnixMilli()))
				counts[m.State]++
				p.HSet(ctx, DeviceState(id), "state", string(m.State), "since", m.Since.UnixMilli())
			}
			p.HSet(ctx, FleetState(fleet), entries...)
			for s, n := range counts {
				p.HSet(ctx, FleetCounts(fleet), string(s), n)
			}
		}
		return nil
	})
	return err
}
