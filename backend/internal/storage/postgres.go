package storage

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"turbodriver/internal/dispatch"
)

type Postgres struct {
	pool *pgxpool.Pool
}

func NewPostgres(pool *pgxpool.Pool) *Postgres {
	return &Postgres{pool: pool}
}

// EnsureSchema creates minimal tables for rides and drivers if they do not exist.
func EnsureSchema(ctx context.Context, pool *pgxpool.Pool) error {
	return ApplySchema(ctx, pool)
}

func (p *Postgres) SaveDriver(d dispatch.DriverState) error {
	_, err := p.pool.Exec(context.Background(), `
INSERT INTO drivers (id, latitude, longitude, accuracy, ts, status, ride_id, radius_km, available, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
ON CONFLICT (id) DO UPDATE SET
	latitude = EXCLUDED.latitude,
	longitude = EXCLUDED.longitude,
	accuracy = EXCLUDED.accuracy,
	ts = EXCLUDED.ts,
	status = EXCLUDED.status,
	ride_id = EXCLUDED.ride_id,
	radius_km = EXCLUDED.radius_km,
	available = EXCLUDED.available,
	updated_at = EXCLUDED.updated_at
`, d.ID, d.Location.Latitude, d.Location.Longitude, d.Location.Accuracy, d.Location.At, d.Status, d.RideID, d.RadiusKM, d.Available, d.UpdatedAt)
	return err
}

func (p *Postgres) SaveRide(r dispatch.Ride) error {
	_, err := p.pool.Exec(context.Background(), `
INSERT INTO rides (id, passenger_id, driver_id, status, pickup_lat, pickup_long, pickup_accuracy, pickup_ts, created_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
ON CONFLICT (id) DO UPDATE SET
	driver_id = EXCLUDED.driver_id,
	status = EXCLUDED.status
`, r.ID, r.PassengerID, r.DriverID, r.Status, r.Pickup.Latitude, r.Pickup.Longitude, r.Pickup.Accuracy, r.Pickup.At, r.CreatedAt)
	return err
}

func (p *Postgres) UpdateRideStatus(id string, status dispatch.RideStatus) error {
	_, err := p.pool.Exec(context.Background(), `
UPDATE rides SET status = $2 WHERE id = $1
`, id, status)
	return err
}

func (p *Postgres) SetDriverRide(driverID, rideID, status string, available bool) error {
	_, err := p.pool.Exec(context.Background(), `
UPDATE drivers SET ride_id = $2, status = $3, available = $4 WHERE id = $1
`, driverID, rideID, status, available)
	return err
}

func (p *Postgres) GetRide(id string) (dispatch.Ride, bool, error) {
	row := p.pool.QueryRow(context.Background(), `
SELECT id, passenger_id, driver_id, status, pickup_lat, pickup_long, pickup_accuracy, pickup_ts, created_at
FROM rides WHERE id = $1
`, id)
	var (
		ride dispatch.Ride
		acc  *float64
	)
	err := row.Scan(&ride.ID, &ride.PassengerID, &ride.DriverID, &ride.Status, &ride.Pickup.Latitude, &ride.Pickup.Longitude, &acc, &ride.Pickup.At, &ride.CreatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return dispatch.Ride{}, false, nil
		}
		return dispatch.Ride{}, false, err
	}
	if acc != nil {
		ride.Pickup.Accuracy = *acc
	}
	return ride, true, nil
}

func (p *Postgres) ListRidesByPassenger(ctx context.Context, passengerID string, limit, offset int) ([]dispatch.Ride, error) {
	rows, err := p.pool.Query(ctx, `
SELECT id, passenger_id, driver_id, status, pickup_lat, pickup_long, pickup_accuracy, pickup_ts, created_at
FROM rides
WHERE passenger_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3
`, passengerID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var rides []dispatch.Ride
	for rows.Next() {
		var r dispatch.Ride
		var acc *float64
		if err := rows.Scan(&r.ID, &r.PassengerID, &r.DriverID, &r.Status, &r.Pickup.Latitude, &r.Pickup.Longitude, &acc, &r.Pickup.At, &r.CreatedAt); err != nil {
			return nil, err
		}
		if acc != nil {
			r.Pickup.Accuracy = *acc
		}
		rides = append(rides, r)
	}
	return rides, rows.Err()
}

func (p *Postgres) ListRidesByDriver(ctx context.Context, driverID string, limit, offset int) ([]dispatch.Ride, error) {
	rows, err := p.pool.Query(ctx, `
SELECT id, passenger_id, driver_id, status, pickup_lat, pickup_long, pickup_accuracy, pickup_ts, created_at
FROM rides
WHERE driver_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3
`, driverID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var rides []dispatch.Ride
	for rows.Next() {
		var r dispatch.Ride
		var acc *float64
		if err := rows.Scan(&r.ID, &r.PassengerID, &r.DriverID, &r.Status, &r.Pickup.Latitude, &r.Pickup.Longitude, &acc, &r.Pickup.At, &r.CreatedAt); err != nil {
			return nil, err
		}
		if acc != nil {
			r.Pickup.Accuracy = *acc
		}
		rides = append(rides, r)
	}
	return rides, rows.Err()
}

func (p *Postgres) CountRidesByPassenger(ctx context.Context, passengerID string) (int, error) {
	var count int
	if err := p.pool.QueryRow(ctx, `SELECT COUNT(*) FROM rides WHERE passenger_id = $1`, passengerID).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (p *Postgres) CountRidesByDriver(ctx context.Context, driverID string) (int, error) {
	var count int
	if err := p.pool.QueryRow(ctx, `SELECT COUNT(*) FROM rides WHERE driver_id = $1`, driverID).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func DefaultPool(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	cfg.MaxConnLifetime = time.Hour
	return pgxpool.NewWithConfig(ctx, cfg)
}
