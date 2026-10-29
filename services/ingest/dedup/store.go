package dedup

import (
	"context"
	"fmt"
	"github.com/alyfarghali/spincycle/services/ingest/decoder"
	"github.com/alyfarghali/spincycle/services/ingest/engine"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) Save(ctx context.Context, r decoder.Reading, transitions []engine.Transition) (stored bool, err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin: %w", err)
	}
	// No-op after Commit; undoes everything on any early return.
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx,
		`INSERT INTO readings_seen (device_id, boot, seq) VALUES ($1, $2, $3)
		 ON CONFLICT DO NOTHING`,
		r.DeviceID, int64(r.Boot), int64(r.Seq))
	if err != nil {
		return false, fmt.Errorf("insert readings_seen: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return false, nil // already stored: a duplicate
	}

	for metric, value := range r.Metrics {
		if _, err := tx.Exec(ctx,
			`INSERT INTO telemetry (time, device_id, metric, value) VALUES ($1, $2, $3, $4)`,
			r.Time, r.DeviceID, metric, value); err != nil {
			return false, fmt.Errorf("insert telemetry: %w", err)
		}
	}

	// Same transaction: a crash in between would lose the transition, because the redelivered
	// reading would then be dropped as a duplicate.
	if err := insertTransitions(ctx, tx, r.DeviceID, transitions); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit: %w", err)
	}
	return true, nil
}

func (s *Store) SaveTransitions(ctx context.Context, deviceID string, transitions []engine.Transition) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := insertTransitions(ctx, tx, deviceID, transitions); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func insertTransitions(ctx context.Context, tx pgx.Tx, deviceID string, transitions []engine.Transition) error {
	for _, t := range transitions {
		if _, err := tx.Exec(ctx,
			`INSERT INTO machine_events (device_id, from_state, to_state, at) VALUES ($1, $2, $3, $4)`,
			deviceID, string(t.From), string(t.To), t.At); err != nil {
			return fmt.Errorf("insert machine_events: %w", err)
		}
	}
	return nil
}

func (s *Store) LoadStates(ctx context.Context, now time.Time) (map[string]engine.Machine, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT ON (device_id) device_id, from_state, to_state, at
		FROM machine_events
		ORDER BY device_id, at DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("query machine_events: %w", err)
	}
	defer rows.Close()

	states := map[string]engine.Machine{}
	for rows.Next() {
		var id, from, to string
		var at time.Time
		if err := rows.Scan(&id, &from, &to, &at); err != nil {
			return nil, fmt.Errorf("scan machine_events: %w", err)
		}
		// LastReading = now: after a restart every device gets a full offline_after before it's marked offline.
		m := engine.Machine{State: engine.State(to), Since: at, LastReading: now}
		if m.State == engine.Offline {
			m.Prev, m.PrevSince = engine.State(from), at // the previous state's real start isn't stored
		}
		states[id] = m
	}
	return states, rows.Err()
}
