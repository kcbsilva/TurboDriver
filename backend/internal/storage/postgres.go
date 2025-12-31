package storage

import (
	"context"
	"encoding/json"
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

// Driver application persistence

func (p *Postgres) UpsertDriverApplication(ctx context.Context, app dispatch.DriverApplication) (int64, error) {
	var id int64
	err := p.pool.QueryRow(ctx, `
INSERT INTO driver_applications (driver_id, location_code, rules_version_id, status, created_at, updated_at)
VALUES ($1,$2,$3,$4,NOW(),NOW())
ON CONFLICT (driver_id) DO UPDATE SET
  location_code = EXCLUDED.location_code,
  rules_version_id = EXCLUDED.rules_version_id,
  status = EXCLUDED.status,
  updated_at = NOW()
RETURNING id
`, app.DriverID, app.LocationCode, app.RulesVersion, app.Status).Scan(&id)
	return id, err
}

func (p *Postgres) GetDriverApplication(ctx context.Context, driverID string) (dispatch.DriverApplication, bool, error) {
	var app dispatch.DriverApplication
	var rules *int64
	err := p.pool.QueryRow(ctx, `
SELECT id, driver_id, location_code, rules_version_id, status, created_at, updated_at
FROM driver_applications
WHERE driver_id = $1
`, driverID).Scan(&app.ID, &app.DriverID, &app.LocationCode, &rules, &app.Status, &app.CreatedAt, &app.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return dispatch.DriverApplication{}, false, nil
		}
		return dispatch.DriverApplication{}, false, err
	}
	app.RulesVersion = rules
	return app, true, nil
}

func (p *Postgres) UpdateApplicationStatus(ctx context.Context, driverID string, status dispatch.DriverApplicationStatus) error {
	_, err := p.pool.Exec(ctx, `
UPDATE driver_applications SET status = $2, updated_at = NOW() WHERE driver_id = $1
`, driverID, status)
	return err
}

func (p *Postgres) UpsertDriverLicense(ctx context.Context, lic dispatch.DriverLicense) (int64, error) {
	var id int64
	err := p.pool.QueryRow(ctx, `
INSERT INTO driver_licenses (driver_id, license_number, country, region, expires_at, remunerated, document_url, verified_at, created_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NOW(),NOW())
ON CONFLICT (driver_id) DO UPDATE SET
  license_number = EXCLUDED.license_number,
  country = EXCLUDED.country,
  region = EXCLUDED.region,
  expires_at = EXCLUDED.expires_at,
  remunerated = EXCLUDED.remunerated,
  document_url = EXCLUDED.document_url,
  verified_at = EXCLUDED.verified_at,
  updated_at = NOW()
RETURNING id
`, lic.DriverID, lic.Number, lic.Country, lic.Region, lic.ExpiresAt, lic.Remunerated, lic.DocumentURL, lic.VerifiedAt).Scan(&id)
	return id, err
}

func (p *Postgres) UpsertDriverVehicle(ctx context.Context, veh dispatch.DriverVehicle) (int64, error) {
	var id int64
	err := p.pool.QueryRow(ctx, `
INSERT INTO driver_vehicles (driver_id, vehicle_type, plate_number, document_number, document_expires_at, ownership, contract_url, contract_expires_at, document_url, created_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,NOW(),NOW())
ON CONFLICT (driver_id) DO UPDATE SET
  vehicle_type = EXCLUDED.vehicle_type,
  plate_number = EXCLUDED.plate_number,
  document_number = EXCLUDED.document_number,
  document_expires_at = EXCLUDED.document_expires_at,
  ownership = EXCLUDED.ownership,
  contract_url = EXCLUDED.contract_url,
  contract_expires_at = EXCLUDED.contract_expires_at,
  document_url = EXCLUDED.document_url,
  updated_at = NOW()
RETURNING id
`, veh.DriverID, veh.Type, veh.PlateNumber, veh.DocumentNumber, veh.DocumentExpires, veh.Ownership, veh.ContractURL, veh.ContractExpires, veh.DocumentURL).Scan(&id)
	return id, err
}

func (p *Postgres) ReplaceVehiclePhotos(ctx context.Context, vehicleID int64, photos []dispatch.VehiclePhoto) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM vehicle_photos WHERE vehicle_id = $1`, vehicleID); err != nil {
		return err
	}
	for _, ph := range photos {
		if _, err := tx.Exec(ctx, `
INSERT INTO vehicle_photos (vehicle_id, angle, photo_url, created_at)
VALUES ($1,$2,$3,NOW())
`, vehicleID, ph.Angle, ph.PhotoURL); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (p *Postgres) UpsertLiveness(ctx context.Context, liv dispatch.DriverLiveness) (int64, error) {
	var id int64
	seqJSON, _ := json.Marshal(liv.ChallengeSequence)
	err := p.pool.QueryRow(ctx, `
INSERT INTO driver_liveness_checks (driver_id, challenge_sequence, captures, verified, verified_at, created_at)
VALUES ($1,$2,$3,$4,$5,NOW())
ON CONFLICT (driver_id) DO UPDATE SET
  challenge_sequence = EXCLUDED.challenge_sequence,
  captures = EXCLUDED.captures,
  verified = EXCLUDED.verified,
  verified_at = EXCLUDED.verified_at
RETURNING id
`, liv.DriverID, seqJSON, liv.Captures, liv.Verified, liv.VerifiedAt).Scan(&id)
	return id, err
}

func (p *Postgres) LoadApplicationDetails(ctx context.Context, driverID string) (dispatch.DriverApplication, bool, error) {
	app, ok, err := p.GetDriverApplication(ctx, driverID)
	if err != nil || !ok {
		return app, ok, err
	}
	// license
	if err := p.pool.QueryRow(ctx, `
SELECT id, driver_id, license_number, country, region, expires_at, remunerated, document_url, verified_at, created_at, updated_at
FROM driver_licenses WHERE driver_id = $1
`, driverID).Scan(&app.License.ID, &app.License.DriverID, &app.License.Number, &app.License.Country, &app.License.Region, &app.License.ExpiresAt, &app.License.Remunerated, &app.License.DocumentURL, &app.License.VerifiedAt, &app.License.CreatedAt, &app.License.UpdatedAt); err != nil && err != pgx.ErrNoRows {
		return app, false, err
	}
	// vehicle
	if err := p.pool.QueryRow(ctx, `
SELECT id, driver_id, vehicle_type, plate_number, document_number, document_expires_at, ownership, contract_url, contract_expires_at, document_url, created_at, updated_at
FROM driver_vehicles WHERE driver_id = $1
`, driverID).Scan(&app.Vehicle.ID, &app.Vehicle.DriverID, &app.Vehicle.Type, &app.Vehicle.PlateNumber, &app.Vehicle.DocumentNumber, &app.Vehicle.DocumentExpires, &app.Vehicle.Ownership, &app.Vehicle.ContractURL, &app.Vehicle.ContractExpires, &app.Vehicle.DocumentURL, &app.Vehicle.CreatedAt, &app.Vehicle.UpdatedAt); err != nil && err != pgx.ErrNoRows {
		return app, false, err
	}
	if app.Vehicle.ID > 0 {
		rows, err := p.pool.Query(ctx, `
SELECT id, vehicle_id, angle, photo_url, created_at FROM vehicle_photos WHERE vehicle_id = $1
`, app.Vehicle.ID)
		if err != nil {
			return app, false, err
		}
		defer rows.Close()
		for rows.Next() {
			var ph dispatch.VehiclePhoto
			if err := rows.Scan(&ph.ID, &ph.VehicleID, &ph.Angle, &ph.PhotoURL, &ph.CreatedAt); err != nil {
				return app, false, err
			}
			app.Photos = append(app.Photos, ph)
		}
		if err := rows.Err(); err != nil {
			return app, false, err
		}
	}
	// liveness
	var seqRaw []byte
	if err := p.pool.QueryRow(ctx, `
SELECT id, driver_id, challenge_sequence, captures, verified, verified_at, created_at
FROM driver_liveness_checks WHERE driver_id = $1
`, driverID).Scan(&app.Liveness.ID, &app.Liveness.DriverID, &seqRaw, &app.Liveness.Captures, &app.Liveness.Verified, &app.Liveness.VerifiedAt, &app.Liveness.CreatedAt); err != nil && err != pgx.ErrNoRows {
		return app, false, err
	}
	if len(seqRaw) > 0 {
		_ = json.Unmarshal(seqRaw, &app.Liveness.ChallengeSequence)
	}
	return app, true, nil
}

// Passenger profiles

func (p *Postgres) UpsertPassengerProfile(ctx context.Context, prof dispatch.PassengerProfile) (int64, error) {
	var id int64
	err := p.pool.QueryRow(ctx, `
INSERT INTO passenger_profiles (passenger_id, full_name, address, government_id, created_at, updated_at)
VALUES ($1,$2,$3,$4,NOW(),NOW())
ON CONFLICT (passenger_id) DO UPDATE SET
  full_name = EXCLUDED.full_name,
  address = EXCLUDED.address,
  government_id = EXCLUDED.government_id,
  updated_at = NOW()
RETURNING id
`, prof.PassengerID, prof.FullName, prof.Address, prof.GovernmentID).Scan(&id)
	return id, err
}

func (p *Postgres) GetPassengerProfile(ctx context.Context, passengerID string) (dispatch.PassengerProfile, bool, error) {
	var prof dispatch.PassengerProfile
	err := p.pool.QueryRow(ctx, `
SELECT id, passenger_id, full_name, address, government_id, created_at, updated_at
FROM passenger_profiles
WHERE passenger_id = $1
`, passengerID).Scan(&prof.ID, &prof.PassengerID, &prof.FullName, &prof.Address, &prof.GovernmentID, &prof.CreatedAt, &prof.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return dispatch.PassengerProfile{}, false, nil
		}
		return dispatch.PassengerProfile{}, false, err
	}
	return prof, true, nil
}

// Ratings

func (p *Postgres) UpsertRating(ctx context.Context, r dispatch.Rating) error {
	_, err := p.pool.Exec(ctx, `
INSERT INTO ride_ratings (ride_id, rater_role, rater_id, ratee_id, stars, comment, requires_attention, created_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,NOW())
ON CONFLICT (ride_id, rater_role) DO UPDATE SET
  stars = EXCLUDED.stars,
  comment = EXCLUDED.comment,
  requires_attention = EXCLUDED.requires_attention
`, r.RideID, r.RaterRole, r.RaterID, r.RateeID, r.Stars, r.Comment, r.RequiresAttention)
	return err
}

func (p *Postgres) GetRatingsForRide(ctx context.Context, rideID string) ([]dispatch.Rating, error) {
	rows, err := p.pool.Query(ctx, `
SELECT id, ride_id, rater_role, rater_id, ratee_id, stars, comment, requires_attention, created_at
FROM ride_ratings WHERE ride_id = $1
`, rideID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []dispatch.Rating
	for rows.Next() {
		var r dispatch.Rating
		if err := rows.Scan(&r.ID, &r.RideID, &r.RaterRole, &r.RaterID, &r.RateeID, &r.Stars, &r.Comment, &r.RequiresAttention, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (p *Postgres) GetRatingsForProfile(ctx context.Context, profileID string) ([]dispatch.Rating, error) {
	rows, err := p.pool.Query(ctx, `
SELECT id, ride_id, rater_role, rater_id, ratee_id, stars, comment, requires_attention, created_at
FROM ride_ratings WHERE ratee_id = $1
ORDER BY created_at DESC
`, profileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []dispatch.Rating
	for rows.Next() {
		var r dispatch.Rating
		if err := rows.Scan(&r.ID, &r.RideID, &r.RaterRole, &r.RaterID, &r.RateeID, &r.Stars, &r.Comment, &r.RequiresAttention, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func DefaultPool(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	cfg.MaxConnLifetime = time.Hour
	return pgxpool.NewWithConfig(ctx, cfg)
}
