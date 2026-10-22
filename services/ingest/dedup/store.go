<<<<<<< HEAD
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
=======
// Persists last (boot, seq) per device in memory and Redis.

package dedup
>>>>>>> main
