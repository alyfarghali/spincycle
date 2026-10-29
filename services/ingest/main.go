package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/alyfarghali/spincycle/services/ingest/config"
	"github.com/alyfarghali/spincycle/services/ingest/decoder"
	"github.com/alyfarghali/spincycle/services/ingest/dedup"
	"github.com/alyfarghali/spincycle/services/ingest/engine"
	"github.com/alyfarghali/spincycle/services/ingest/livestate"
	"github.com/alyfarghali/spincycle/services/ingest/metrics"
	"github.com/alyfarghali/spincycle/services/ingest/registry"
	"github.com/alyfarghali/spincycle/services/ingest/subscriber"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if err := run(); err != nil {
		slog.Error("ingest stopped", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	stop, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	pool, err := pgxpool.New(stop, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	reg, err := registry.Load(stop, pool)
	if err != nil {
		return err
	}
	slog.Info("registry loaded", "devices", reg.Len())

	store := dedup.NewStore(pool)
	machines, err := store.LoadStates(stop, time.Now())
	if err != nil {
		return err
	}
	slog.Info("machine states restored", "machines", len(machines))

	redisOpts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		return err
	}
	// Short timeouts and no retries: a slow or dead cache must not hold up storing and acking readings.
	redisOpts.DialTimeout, redisOpts.ReadTimeout, redisOpts.WriteTimeout = redisTimeout, redisTimeout, redisTimeout
	redisOpts.MaxRetries = -1
	rdb := redis.NewClient(redisOpts)
	defer rdb.Close()
	counts := metrics.New()
	inbox := make(chan subscriber.Message, subscriber.InboxSize)

	conn, connCancel := context.WithCancel(context.Background())
	defer connCancel()
	cm, err := subscriber.Start(conn, cfg, inbox, stop.Done())
	if err != nil {
		return err
	}

	go logCounts(stop, counts)

	w := worker{reg: reg, store: store, counts: counts, machines: machines, ack: subscriber.Ack,
		live: livestate.New(rdb), liveDirty: true}
	w.rebuildLive(stop, time.Now())
	// In the worker loop, not a goroutine of its own, so the machines map needs no lock.
	tick := time.NewTicker(tickEvery)
	defer tick.Stop()
	for {
		select {
		case <-stop.Done():
			slog.Info("shutting down")
			ctx, done := context.WithTimeout(context.Background(), 5*time.Second)
			defer done()
			_ = cm.Disconnect(ctx)
			slog.Info("final counts", "counts", counts.Snapshot())
			return nil
		case m := <-inbox:
			w.handle(stop, m)
		case now := <-tick.C:
			w.tick(stop, now)
		}
	}
}

// offline_after (60 s) + one tick stays within the 90 s offline-detection target (CLAUDE.md §3).
const tickEvery = 30 * time.Second

const (
	redisTimeout = 300 * time.Millisecond
	redisBackoff = 5 * time.Second
)

type worker struct {
	reg      *registry.Registry
	store    *dedup.Store
	counts   *metrics.Counters
	machines map[string]engine.Machine
	ack      func(subscriber.Message) error

	live          *livestate.Writer
	liveDownUntil time.Time
	liveDirty     bool // a live-state write was missed; rebuild the fleets on the next tick
}

func (w *worker) handle(ctx context.Context, m subscriber.Message) {
	topic := m.Packet.Topic
	if strings.HasSuffix(topic, "/status") {
		w.handleStatus(ctx, m)
		return
	}
	r, reason, dropped := decoder.Decode(topic, m.Packet.Payload, time.Now(), w.reg)
	if dropped > 0 {
		w.counts.Add("undeclared_metric", int64(dropped))
	}

	if reason != decoder.OK {
		// Ack rejects too: they can never become valid, and an unacked
		// message would come back forever (a "poison message").
		w.counts.Inc(string(reason))
		slog.Warn("reading rejected", "reason", reason, "topic", topic)
		w.ackOrLog(m)
		return
	}

	// Only computed here; memory changes after the commit, so a failed save leaves state untouched.
	next, transitions, hasState := w.step(r)
	stored, err := w.saveWithRetry(ctx, r, transitions)
	if err != nil {
		return // stopping: leave unacked for redelivery
	}
	if stored {
		w.counts.Inc("stored")
		if hasState {
			w.apply(r.DeviceID, next, transitions)
		}
		fleet := w.fleetOf(r.DeviceID)
		m := w.machines[r.DeviceID]
		w.writeLive(ctx, func(ctx context.Context) error {
			if err := w.live.Reading(ctx, fleet, r, m); err != nil {
				return err
			}
			return w.live.Transitions(ctx, fleet, r.DeviceID, m, transitions, r.Received)
		})
	} else {
		w.counts.Inc("duplicate")
		slog.Info("duplicate dropped", "device_id", r.DeviceID, "boot", r.Boot, "seq", r.Seq)
	}
	w.ackOrLog(m)
}

func (w *worker) step(r decoder.Reading) (engine.Machine, []engine.Transition, bool) {
	dev, _ := w.reg.Lookup(r.DeviceID)
	rms, ok := r.Metrics["vibration_rms"]
	if dev.EngineErr != nil || !ok {
		return engine.Machine{}, nil, false
	}
	next, transitions := engine.OnReading(w.machines[r.DeviceID], rms, r.Time, dev.Engine)
	return next, transitions, true
}

func (w *worker) handleStatus(ctx context.Context, m subscriber.Message) {
	id, offline, reason := decoder.DecodeStatus(m.Packet.Topic, m.Packet.Payload, w.reg)
	if reason != decoder.OK {
		w.counts.Inc(string(reason))
		w.ackOrLog(m)
		return
	}
	if offline {
		next, transitions := engine.OnOffline(w.machines[id], time.Now())
		if len(transitions) > 0 {
			if err := w.retry(ctx, id, func() error { return w.store.SaveTransitions(ctx, id, transitions) }); err != nil {
				return // stopping: leave unacked for redelivery
			}
			w.apply(id, next, transitions)
			w.writeLive(ctx, func(ctx context.Context) error {
				return w.live.Transitions(ctx, w.fleetOf(id), id, next, transitions, time.Now())
			})
		}
	}
	w.ackOrLog(m)
}

func (w *worker) tick(ctx context.Context, now time.Time) {
	defer w.rebuildLive(ctx, now)
	for id, m := range w.machines {
		dev, ok := w.reg.Lookup(id)
		if !ok || dev.EngineErr != nil {
			continue
		}
		next, transitions := engine.OnTick(m, now, dev.Engine)
		if len(transitions) == 0 {
			continue
		}
		// No retry: on failure memory is unchanged, so the next tick recomputes the same transitions.
		if err := w.store.SaveTransitions(ctx, id, transitions); err != nil {
			w.counts.Inc("db_error")
			slog.Error("save transitions failed", "device_id", id, "err", err)
			continue
		}
		w.apply(id, next, transitions)
		w.writeLive(ctx, func(ctx context.Context) error {
			return w.live.Transitions(ctx, w.fleetOf(id), id, next, transitions, now)
		})
	}
}

func (w *worker) fleetOf(id string) string {
	dev, _ := w.reg.Lookup(id)
	return dev.FleetID
}

// Redis is a cache (CLAUDE.md §8): a failure is counted and skipped, never retried in line.
func (w *worker) writeLive(ctx context.Context, write func(context.Context) error) bool {
	if w.live == nil {
		return false
	}
	if time.Now().Before(w.liveDownUntil) {
		w.counts.Inc("redis_skipped")
		w.liveDirty = true
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, redisTimeout)
	defer cancel()
	if err := write(ctx); err != nil {
		w.counts.Inc("redis_error")
		w.liveDirty = true
		w.liveDownUntil = time.Now().Add(redisBackoff)
		slog.Warn("live state write failed; skipping Redis for a while", "retry_in", redisBackoff.String(), "err", err)
		return false
	}
	return true
}

func (w *worker) rebuildLive(ctx context.Context, now time.Time) {
	if !w.liveDirty {
		return
	}
	if w.writeLive(ctx, func(ctx context.Context) error {
		return w.live.Rebuild(ctx, w.machines, func(id string) (string, bool) {
			dev, ok := w.reg.Lookup(id)
			return dev.FleetID, ok
		})
	}) {
		w.liveDirty = false
	}
}

func (w *worker) apply(id string, next engine.Machine, transitions []engine.Transition) {
	w.machines[id] = next
	for _, t := range transitions {
		w.counts.Inc("transition")
		slog.Info("transition", "device_id", id, "from", t.From, "to", t.To, "at", t.At)
	}
}

func (w *worker) saveWithRetry(ctx context.Context, r decoder.Reading, transitions []engine.Transition) (bool, error) {
	var stored bool
	err := w.retry(ctx, r.DeviceID, func() (err error) {
		stored, err = w.store.Save(ctx, r, transitions)
		return err
	})
	return stored, err
}

func (w *worker) retry(ctx context.Context, deviceID string, save func() error) error {
	delay := time.Second
	for {
		err := save()
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		w.counts.Inc("db_error")
		slog.Error("save failed; retrying", "device_id", deviceID, "retry_in", delay.String(), "err", err)
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return ctx.Err()
		}
		delay = min(delay*2, 30*time.Second)
	}
}

func (w *worker) ackOrLog(m subscriber.Message) {
	err := w.ack(m)
	switch {
	case errors.Is(err, subscriber.ErrStaleConnection):
		w.counts.Inc("ack_stale") // fine: redelivered and deduped
	case err != nil:
		slog.Error("ack failed", "topic", m.Packet.Topic, "err", err)
	}
}

func logCounts(ctx context.Context, counts *metrics.Counters) {
	t := time.NewTicker(60 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			slog.Info("counts", "counts", counts.Snapshot())
		}
	}
}
