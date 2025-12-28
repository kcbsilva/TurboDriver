package storage

import (
	"context"
	"encoding/json"
	"time"
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
	AppendRideEvent(ctx context.Context, evt RideEvent) error
	ListRideEvents(ctx context.Context, rideID string, limit, offset int) ([]RideEvent, error)
}

func (p *Postgres) AppendRideEvent(ctx context.Context, evt RideEvent) error {
	_, err := p.pool.Exec(ctx, `
INSERT INTO ride_events (ride_id, event_type, payload, actor_id, actor_role, created_at)
VALUES ($1,$2,$3,$4,$5,COALESCE($6,NOW()))
`, evt.RideID, evt.Type, evt.Payload, evt.ActorID, evt.ActorRole, evt.CreatedAt)
	return err
}

func (p *Postgres) ListRideEvents(ctx context.Context, rideID string, limit, offset int) ([]RideEvent, error) {
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
	var out []RideEvent
	for rows.Next() {
		var evt RideEvent
		if err := rows.Scan(&evt.RideID, &evt.Type, &evt.Payload, &evt.ActorID, &evt.ActorRole, &evt.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, evt)
	}
	return out, rows.Err()
}
