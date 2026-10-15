// Postgres write path: records the reading's ID in readings_seen and its rows in telemetry in one transaction.
// readings_seen is the authority for duplicates (contract A8); a Redis pre-filter can sit in front of it later.

package dedup

import (
	"context"
	"fmt"
	"github.com/alyfarghali/spincycle/services/ingest/decoder"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Save stores a reading unless (device_id, boot, seq) was already stored.
// stored is false for a duplicate. Marking it seen and writing its rows
// happen in one transaction, so a crash can never leave a reading marked
// seen but not stored (that reading would be lost on redelivery).
func (s *Store) Save(ctx context.Context, r decoder.Reading) (stored bool, err error) {
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

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit: %w", err)
	}
	return true, nil
}
