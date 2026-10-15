// In-memory device registry (device -> fleet, type, declared metrics), loaded from Postgres at startup.
// Bare version: restart ingest to pick up newly registered devices. Periodic refresh comes later.

package registry

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Device struct {
	ID      string
	FleetID string
	TypeID  string
	Metrics map[string]bool // metric names the device's type declares (contract Part B)
}

type Registry struct {
	devices map[string]Device
}

// Load reads every device that isn't soft-deleted, with its type's metric list.
func Load(ctx context.Context, pool *pgxpool.Pool) (*Registry, error) {
	rows, err := pool.Query(ctx, `
		SELECT d.id, d.fleet_id, d.type_id, t.metrics
		FROM devices d
		JOIN device_types t ON t.id = d.type_id
		WHERE d.deleted_at IS NULL`)
	if err != nil {
		return nil, fmt.Errorf("query devices: %w", err)
	}
	defer rows.Close()

	r := &Registry{devices: make(map[string]Device)}
	for rows.Next() {
		var d Device
		var metrics []string // device_types.metrics is a JSON array of names
		if err := rows.Scan(&d.ID, &d.FleetID, &d.TypeID, &metrics); err != nil {
			return nil, fmt.Errorf("scan device: %w", err)
		}
		d.Metrics = make(map[string]bool, len(metrics))
		for _, m := range metrics {
			d.Metrics[m] = true
		}
		r.devices[d.ID] = d
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read devices: %w", err)
	}
	return r, nil
}

// New builds a registry from a fixed list. Used by tests.
func New(devices ...Device) *Registry {
	r := &Registry{devices: make(map[string]Device, len(devices))}
	for _, d := range devices {
		r.devices[d.ID] = d
	}
	return r
}

func (r *Registry) Lookup(id string) (Device, bool) {
	d, ok := r.devices[id]
	return d, ok
}

func (r *Registry) Len() int { return len(r.devices) }
