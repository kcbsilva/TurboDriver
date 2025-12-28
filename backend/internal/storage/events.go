package storage

import (
	"context"
	"encoding/json"
	"time"

	"turbodriver/internal/dispatch"
)

type RideEvent struct {
	RideID    string          `json:"rideId"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	ActorID   string          `json:"actorId,omitempty"`
	ActorRole string          `json:"actorRole,omitempty"`
	CreatedAt time.Time       `json:"createdAt"`
}

type EventLogger interface {
	AppendRideEvent(ctx context.Context, evt dispatch.RideEvent) error
	ListRideEvents(ctx context.Context, rideID string, limit, offset int) ([]dispatch.RideEvent, error)
	CountRideEvents(ctx context.Context, rideID string) (int, error)
}

func (p *Postgres) AppendRideEvent(ctx context.Context, evt dispatch.RideEvent) error {
	_, err := p.pool.Exec(ctx, `
INSERT INTO ride_events (ride_id, event_type, payload, actor_id, actor_role, created_at)
VALUES ($1,$2,$3,$4,$5,COALESCE($6,NOW()))
`, evt.RideID, evt.Type, evt.Payload, evt.ActorID, evt.ActorRole, evt.CreatedAt)
	return err
}

func (p *Postgres) ListRideEvents(ctx context.Context, rideID string, limit, offset int) ([]dispatch.RideEvent, error) {
	rows, err := p.pool.Query(ctx, `
SELECT ride_id, event_type, payload, actor_id, actor_role, created_at
FROM ride_events
WHERE ride_id = $1
ORDER BY created_at ASC
LIMIT $2 OFFSET $3
`, rideID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []dispatch.RideEvent
	for rows.Next() {
		var evt dispatch.RideEvent
		if err := rows.Scan(&evt.RideID, &evt.Type, &evt.Payload, &evt.ActorID, &evt.ActorRole, &evt.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, evt)
	}
	return out, rows.Err()
}

func (p *Postgres) CountRideEvents(ctx context.Context, rideID string) (int, error) {
	var count int
	if err := p.pool.QueryRow(ctx, `SELECT COUNT(*) FROM ride_events WHERE ride_id = $1`, rideID).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (p *Postgres) CreateRideWithEvent(ctx context.Context, ride dispatch.Ride, event dispatch.RideEvent, driver dispatch.DriverState) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
INSERT INTO rides (id, passenger_id, driver_id, status, pickup_lat, pickup_long, pickup_accuracy, pickup_ts, created_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
ON CONFLICT (id) DO UPDATE SET driver_id = EXCLUDED.driver_id, status = EXCLUDED.status
`, ride.ID, ride.PassengerID, ride.DriverID, ride.Status, ride.Pickup.Latitude, ride.Pickup.Longitude, ride.Pickup.Accuracy, ride.Pickup.At, ride.CreatedAt); err != nil {
		return err
	}
	if driver.ID != "" {
		if _, err := tx.Exec(ctx, `
UPDATE drivers SET ride_id=$2, status=$3, available=$4 WHERE id=$1
`, driver.ID, driver.RideID, driver.Status, driver.Available); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO ride_events (ride_id, event_type, payload, actor_id, actor_role, created_at)
VALUES ($1,$2,$3,$4,$5,COALESCE($6,NOW()))
`, event.RideID, event.Type, event.Payload, event.ActorID, event.ActorRole, event.CreatedAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (p *Postgres) UpdateRideWithEvent(ctx context.Context, ride dispatch.Ride, event dispatch.RideEvent, driver *dispatch.DriverState) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
UPDATE rides SET driver_id=$2, status=$3 WHERE id=$1
`, ride.ID, ride.DriverID, ride.Status); err != nil {
		return err
	}
	if driver != nil {
		if _, err := tx.Exec(ctx, `
UPDATE drivers SET ride_id=$2, status=$3, available=$4 WHERE id=$1
`, driver.ID, driver.RideID, driver.Status, driver.Available); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO ride_events (ride_id, event_type, payload, actor_id, actor_role, created_at)
VALUES ($1,$2,$3,$4,$5,COALESCE($6,NOW()))
`, event.RideID, event.Type, event.Payload, event.ActorID, event.ActorRole, event.CreatedAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
