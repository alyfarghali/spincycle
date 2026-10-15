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
	Metrics map[string]bool
}

type Registry struct {
	devices map[string]Device
}

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
		var metrics []string
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
