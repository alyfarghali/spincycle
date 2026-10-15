// Wires the ingest pipeline together and handles shutdown.
// Bare version (load-test baseline): one worker, one transaction per message, no batching.
//
//	broker → subscriber callback → inbox → worker: decode → save → ack

package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/alyfarghali/spincycle/services/ingest/config"
	"github.com/alyfarghali/spincycle/services/ingest/decoder"
	"github.com/alyfarghali/spincycle/services/ingest/dedup"
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

	// Ctrl-C or `docker stop` cancels stop. The broker connection gets its own
	// context so it stays up until the worker has finished its last message.
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
	counts := metrics.New()
	inbox := make(chan subscriber.Message, subscriber.InboxSize)

	conn, connCancel := context.WithCancel(context.Background())
	defer connCancel()
	cm, err := subscriber.Start(conn, cfg, inbox, stop.Done())
	if err != nil {
		return err
	}

	go logCounts(stop, counts)

	w := worker{reg: reg, store: store, counts: counts}
	for {
		select {
		case <-stop.Done():
			slog.Info("shutting down")
			// Unacked messages stay in our broker session and come back next run.
			ctx, done := context.WithTimeout(context.Background(), 5*time.Second)
			defer done()
			_ = cm.Disconnect(ctx)
			slog.Info("final counts", "counts", counts.Snapshot())
			return nil
		case m := <-inbox:
			w.handle(stop, m)
		}
	}
}

type worker struct {
	reg    *registry.Registry
	store  *dedup.Store
	counts *metrics.Counters
}

// handle processes one message. It acks only when the message is safely
// stored, a duplicate, or rejected for good. If ingest is stopping before
// the save succeeds, the message is left unacked and the broker redelivers it.
func (w *worker) handle(ctx context.Context, m subscriber.Message) {
	topic := m.Packet.Topic
	r, reason, dropped := decoder.Decode(topic, m.Packet.Payload, time.Now(), w.reg)
	if dropped > 0 {
		w.counts.Add("undeclared_metric", int64(dropped))
	}

	if reason != decoder.OK {
		// Ack rejects too: they can never become valid, and an unacked
		// message would come back forever (a "poison message").
		w.counts.Inc(string(reason))
		slog.Warn("reading rejected", "reason", reason, "topic", topic)
		w.ack(m)
		return
	}

	stored, err := w.saveWithRetry(ctx, r)
	if err != nil {
		return // stopping: leave unacked for redelivery
	}
	if stored {
		w.counts.Inc("stored")
	} else {
		w.counts.Inc("duplicate")
		slog.Info("duplicate dropped", "device_id", r.DeviceID, "boot", r.Boot, "seq", r.Seq)
	}
	w.ack(m)
}

// saveWithRetry retries database errors with backoff (1 s, 2 s, 4 s … 30 s)
// until the save succeeds or ctx is cancelled. Moving on without saving would
// mean either acking an unsaved reading (lost) or not acking it (stuck).
func (w *worker) saveWithRetry(ctx context.Context, r decoder.Reading) (bool, error) {
	delay := time.Second
	for {
		stored, err := w.store.Save(ctx, r)
		if err == nil {
			return stored, nil
		}
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		w.counts.Inc("db_error")
		slog.Error("save failed; retrying", "device_id", r.DeviceID, "retry_in", delay.String(), "err", err)
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return false, ctx.Err()
		}
		delay = min(delay*2, 30*time.Second)
	}
}

func (w *worker) ack(m subscriber.Message) {
	err := subscriber.Ack(m)
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
