package dispatch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"
)

// Persistence allows persisting and retrieving driver/ride state.
type Persistence interface {
	SaveDriver(DriverState) error
	SaveRide(Ride) error
	UpdateRideStatus(id string, status RideStatus) error
	SetDriverRide(driverID, rideID, status string, available bool) error
	GetRide(string) (Ride, bool, error)
}

// Store keeps a minimal in-memory view of drivers and rides, with optional persistence.
type Store struct {
	mu          sync.RWMutex
	drivers     map[string]DriverState
	rides       map[string]Ride
	persistence Persistence
	geo         GeoLocator
	tx          RideTransaction
	pruneCount  int64
	lastPruned  int64
	staleCount  int64
	idemCache   *idemCache
	idemDB      IdempotencyStore
	dbPing      func(context.Context) error
	redisPing   func(context.Context) error
}

func NewStore() *Store {
	return NewStoreWithPersistence(nil)
}

type GeoLocator interface {
	Nearby(lat, lon, radiusKM float64) (string, float64, error)
	Add(driverID string, lat, lon float64) error
	Remove(driverID string) error
	PruneOlderThan(cutoff time.Time)
}

func NewStoreWithPersistence(p Persistence) *Store {
	return NewStoreWithDeps(p, nil)
}

func NewStoreWithDeps(p Persistence, g GeoLocator) *Store {
	return &Store{
		drivers:     make(map[string]DriverState),
		rides:       make(map[string]Ride),
		persistence: p,
		geo:         g,
		tx:          toRideTx(p),
		idemCache:   newIdemCache(),
	}
}

func toRideTx(p Persistence) RideTransaction {
	if tx, ok := p.(RideTransaction); ok {
		return tx
	}
	return nil
}

// AttachIdempotency connects a persistent idempotency store.
func (s *Store) AttachIdempotency(store IdempotencyStore) {
	s.idemDB = store
}

// AttachHealth sets ping functions used by readiness checks.
func (s *Store) AttachHealth(db func(context.Context) error, redis func(context.Context) error) {
	s.dbPing = db
	s.redisPing = redis
}

// UpdateDriverLocation sets the latest known driver position and marks them available.
func (s *Store) UpdateDriverLocation(id string, loc Coordinate) (DriverState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := DriverState{
		ID:        id,
		Available: true,
		Location:  loc,
		UpdatedAt: time.Now(),
		Status:    "idle",
		RadiusKM:  3,
	}
	if existing, ok := s.drivers[id]; ok {
		state.RideID = existing.RideID
		if existing.RideID != "" {
			state.Status = "on_ride"
			state.Available = false
		}
	}
	s.drivers[id] = state
	if s.persistence != nil {
		if err := s.persistence.SaveDriver(state); err != nil {
			return state, err
		}
	}
	if s.geo != nil {
		_ = s.geo.Add(id, loc.Latitude, loc.Longitude)
	}
	return state, nil
}

// CreateRide creates a ride and assigns the nearest available driver within a fixed radius.
func (s *Store) CreateRide(passengerID string, pickup Coordinate, idemKey string) (Ride, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if idemKey != "" {
		if ride, ok := s.lookupRideByKeyLocked(idemKey); ok {
			return ride, nil
		}
	}

	nearestID, dist := s.findNearestDriverLocked(pickup, 3)
	if nearestID == "" {
		return Ride{}, errors.New("no nearby drivers available")
	}

	now := time.Now()
	ride := Ride{
		ID:          fmt.Sprintf("ride_%d", now.UnixNano()),
		PassengerID: passengerID,
		DriverID:    nearestID,
		Status:      RideAssigned,
		Pickup:      pickup,
		CreatedAt:   now,
	}

	driver := s.drivers[nearestID]
	driver.RideID = ride.ID
	driver.Status = "assigned"
	driver.Available = false

	s.drivers[nearestID] = driver
	s.rides[ride.ID] = ride

	s.persistRideAndDriverTx(ride, driver, "ride_assigned", map[string]any{
		"statusTo": ride.Status,
		"driverId": driver.ID,
		"distKm":   dist,
	})
	s.idemCache.Remember(idemKey, ride.ID)
	if s.idemDB != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		_ = s.idemDB.Remember(ctx, idemKey, ride.ID)
	}

	_ = dist // retained for future logging/metrics
	return ride, nil
}

// LookupIdempotent returns a ride if the idempotency key was seen.
func (s *Store) LookupIdempotent(key string) (Ride, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lookupRideByKeyLocked(key)
}

func (s *Store) lookupRideByKeyLocked(key string) (Ride, bool) {
	if key == "" {
		return Ride{}, false
	}
	if id, ok := s.idemCache.Lookup(key); ok {
		return s.GetRide(id)
	}
	if s.idemDB != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		if id, ok, err := s.idemDB.Lookup(ctx, key); err == nil && ok {
			return s.GetRide(id)
		}
	}
	return Ride{}, false
}

func (s *Store) GetRide(id string) (Ride, bool) {
	s.mu.RLock()
	ride, ok := s.rides[id]
	s.mu.RUnlock()
	if ok {
		return ride, true
	}
	if s.persistence != nil {
		dbRide, found, err := s.persistence.GetRide(id)
		if err == nil && found {
			s.mu.Lock()
			s.rides[id] = dbRide
			s.mu.Unlock()
			return dbRide, true
		}
	}
	return Ride{}, false
}

// AcceptRide transitions a ride to accepted and marks the driver as busy.
func (s *Store) AcceptRide(rideID, driverID string) (Ride, RideStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ride, ok := s.rides[rideID]
	if !ok {
		return Ride{}, "", errors.New("ride not found")
	}
	if ride.DriverID != driverID {
		return Ride{}, "", errors.New("driver mismatch")
	}
	if ride.Status != RideAssigned {
		return Ride{}, "", errors.New("ride not in assignable state")
	}

	prev := ride.Status
	ride.Status = RideAccepted
	s.rides[rideID] = ride

	driver := s.drivers[driverID]
	driver.Status = "accepted"
	driver.Available = false
	driver.RideID = ride.ID
	s.drivers[driverID] = driver

	s.persistRideAndDriverTx(ride, driver, "ride_accepted", map[string]any{
		"statusFrom": prev,
		"statusTo":   ride.Status,
	})
	return ride, prev, nil
}

// CancelRide cancels a ride and frees the driver.
func (s *Store) CancelRide(rideID string) (Ride, RideStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ride, ok := s.rides[rideID]
	if !ok {
		return Ride{}, "", errors.New("ride not found")
	}
	if ride.Status == RideCancelled || ride.Status == RideComplete {
		return Ride{}, "", errors.New("ride already finished")
	}

	prev := ride.Status
	ride.Status = RideCancelled
	s.rides[rideID] = ride

	if ride.DriverID != "" {
		driver := s.drivers[ride.DriverID]
		driver.Status = "idle"
		driver.Available = true
		driver.RideID = ""
		s.drivers[driver.ID] = driver
		s.persistRideAndDriverTx(ride, driver, "ride_cancelled", map[string]any{
			"statusFrom": prev,
			"statusTo":   ride.Status,
		})
	} else {
		s.persistRideAndDriverTx(ride, DriverState{}, "ride_cancelled", map[string]any{
			"statusFrom": prev,
			"statusTo":   ride.Status,
		})
	}

	return ride, prev, nil
}

// CompleteRide marks a ride complete and frees the driver.
func (s *Store) CompleteRide(rideID string) (Ride, RideStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ride, ok := s.rides[rideID]
	if !ok {
		return Ride{}, "", errors.New("ride not found")
	}
	if ride.Status != RideAccepted && ride.Status != RideEnRoute {
		return Ride{}, "", errors.New("ride not in progress")
	}

	prev := ride.Status
	ride.Status = RideComplete
	s.rides[rideID] = ride

	if ride.DriverID != "" {
		driver := s.drivers[ride.DriverID]
		driver.Status = "idle"
		driver.Available = true
		driver.RideID = ""
		s.drivers[driver.ID] = driver
		s.persistRideAndDriverTx(ride, driver, "ride_completed", map[string]any{
			"statusFrom": prev,
			"statusTo":   ride.Status,
		})
	} else {
		s.persistRideAndDriverTx(ride, DriverState{}, "ride_completed", map[string]any{
			"statusFrom": prev,
			"statusTo":   ride.Status,
		})
	}

	return ride, prev, nil
}

// UpdateRideStatus allows direct status updates used by persistence or admin overrides.
func (s *Store) UpdateRideStatus(rideID string, status RideStatus) (Ride, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ride, ok := s.rides[rideID]
	if !ok {
		return Ride{}, errors.New("ride not found")
	}
	ride.Status = status
	s.rides[rideID] = ride
	s.persistRideAndDriver(ride, DriverState{})
	return ride, nil
}

func (s *Store) persistRideAndDriver(ride Ride, driver DriverState) {
	if s.persistence == nil {
		return
	}
	_ = s.persistence.UpdateRideStatus(ride.ID, ride.Status)
	if driver.ID != "" {
		_ = s.persistence.SetDriverRide(driver.ID, driver.RideID, driver.Status, driver.Available)
	}
}

func (s *Store) persistRideAndDriverTx(ride Ride, driver DriverState, evt string, payload map[string]any) {
	if s.tx != nil {
		body, _ := json.Marshal(payload)
		var drv *DriverState
		if driver.ID != "" {
			drv = &driver
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if evt == "ride_assigned" && payload["statusFrom"] == nil {
			_ = s.tx.CreateRideWithEvent(ctx, ride, RideEvent{
				RideID:    ride.ID,
				Type:      evt,
				Payload:   body,
				CreatedAt: time.Now(),
			}, driver)
			return
		}
		_ = s.tx.UpdateRideWithEvent(ctx, ride, RideEvent{
			RideID:    ride.ID,
			Type:      evt,
			Payload:   body,
			CreatedAt: time.Now(),
		}, drv)
		return
	}
	s.persistRideAndDriver(ride, driver)
}

// PruneStaleDrivers removes drivers whose heartbeats are older than ttl.
func (s *Store) PruneStaleDrivers(ttl time.Duration) {
	cutoff := time.Now().Add(-ttl)
	s.mu.Lock()
	defer s.mu.Unlock()
	var removed int64
	var stale int64
	for id, driver := range s.drivers {
		if driver.UpdatedAt.Before(cutoff) && driver.RideID == "" {
			delete(s.drivers, id)
			if s.geo != nil {
				_ = s.geo.Remove(id)
				s.geo.PruneOlderThan(cutoff)
			}
			removed++
		}
		if driver.UpdatedAt.Before(cutoff) {
			stale++
		}
	}
	if removed > 0 {
		atomic.AddInt64(&s.pruneCount, removed)
	}
	atomic.StoreInt64(&s.lastPruned, removed)
	atomic.StoreInt64(&s.staleCount, stale)
}

// PruneCount returns number of drivers pruned since start.
func (s *Store) PruneCount() int64 {
	return atomic.LoadInt64(&s.pruneCount)
}

// LastPruned returns drivers removed in last prune cycle.
func (s *Store) LastPruned() int64 {
	return atomic.LoadInt64(&s.lastPruned)
}

// StaleCount returns drivers whose heartbeat is older than TTL.
func (s *Store) StaleCount() int64 {
	return atomic.LoadInt64(&s.staleCount)
}

// SnapshotDrivers returns counts of total, available, and stale (older than ttl).
func (s *Store) SnapshotDrivers(ttl time.Duration) (int, int, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var total, available, stale int
	cutoff := time.Now().Add(-ttl)
	for _, d := range s.drivers {
		total++
		if d.Available {
			available++
		}
		if ttl > 0 && d.UpdatedAt.Before(cutoff) {
			stale++
		}
	}
	return total, available, stale
}

// DriverIsFresh checks if driver heartbeat is within ttl.
func (s *Store) DriverIsFresh(driverID string, ttl time.Duration) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	drv, ok := s.drivers[driverID]
	if !ok {
		return false
	}
	return time.Since(drv.UpdatedAt) <= ttl
}

// HealthCheck checks db/redis ping if configured.
func (s *Store) HealthCheck(ctx context.Context) error {
	if s.dbPing != nil {
		if err := s.dbPing(ctx); err != nil {
			return err
		}
	}
	if s.redisPing != nil {
		if err := s.redisPing(ctx); err != nil {
			return err
		}
	}
	return nil
}

// ReassignIfUnaccepted frees the current driver and attempts to reassign if still unaccepted.
func (s *Store) ReassignIfUnaccepted(rideID, expectedDriverID string) (Ride, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ride, ok := s.rides[rideID]
	if !ok {
		return Ride{}, false, errors.New("ride not found")
	}
	if ride.Status != RideAssigned || ride.DriverID != expectedDriverID {
		return ride, false, nil
	}

	// free prior driver
	if driver, ok := s.drivers[expectedDriverID]; ok {
		driver.Status = "idle"
		driver.Available = true
		driver.RideID = ""
		s.drivers[driver.ID] = driver
		s.persistRideAndDriver(ride, driver)
	}

	exclude := map[string]struct{}{expectedDriverID: {}}
	nextID, _ := s.findNearestDriverLockedExcluding(ride.Pickup, 3, exclude)
	if nextID == "" {
		ride.Status = RideRequested
		ride.DriverID = ""
		s.rides[rideID] = ride
		s.persistRideAndDriver(ride, DriverState{})
		return ride, true, nil
	}

	ride.DriverID = nextID
	ride.Status = RideAssigned
	s.rides[rideID] = ride

	driver := s.drivers[nextID]
	driver.RideID = ride.ID
	driver.Status = "assigned"
	driver.Available = false
	s.drivers[nextID] = driver
	s.persistRideAndDriver(ride, driver)
	return ride, true, nil
}

func (s *Store) findNearestDriverLocked(target Coordinate, radiusKM float64) (string, float64) {
	return s.findNearestDriverLockedExcluding(target, radiusKM, nil)
}

func (s *Store) findNearestDriverLockedExcluding(target Coordinate, radiusKM float64, exclude map[string]struct{}) (string, float64) {
	if s.geo != nil {
		id, dist, err := s.geo.Nearby(target.Latitude, target.Longitude, radiusKM)
		if err == nil {
			if _, skip := exclude[id]; skip {
				// fall back to scan
			} else if driver, ok := s.drivers[id]; ok && driver.Available {
				return id, dist
			}
		}
	}
	var bestID string
	bestDist := math.MaxFloat64
	for id, driver := range s.drivers {
		if exclude != nil {
			if _, skip := exclude[id]; skip {
				continue
			}
		}
		if !driver.Available {
			continue
		}
		dist := haversineKM(target, driver.Location)
		if dist <= radiusKM && dist < bestDist {
			bestID = id
			bestDist = dist
		}
	}
	return bestID, bestDist
}

func haversineKM(a, b Coordinate) float64 {
	const earthRadiusKM = 6371
	lat1 := toRadians(a.Latitude)
	lat2 := toRadians(b.Latitude)
	dLat := toRadians(b.Latitude - a.Latitude)
	dLon := toRadians(b.Longitude - a.Longitude)

	sinLat := math.Sin(dLat / 2)
	sinLon := math.Sin(dLon / 2)

	calc := sinLat*sinLat + math.Cos(lat1)*math.Cos(lat2)*sinLon*sinLon
	return 2 * earthRadiusKM * math.Asin(math.Sqrt(calc))
}

func toRadians(deg float64) float64 {
	return deg * math.Pi / 180
}
