package dispatch

import (
	"errors"
	"fmt"
	"math"
	"sync"
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
}

func NewStore() *Store {
	return NewStoreWithPersistence(nil)
}

type GeoLocator interface {
	Nearby(lat, lon, radiusKM float64) (string, float64, error)
	Add(driverID string, lat, lon float64) error
	Remove(driverID string) error
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
	}
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
func (s *Store) CreateRide(passengerID string, pickup Coordinate) (Ride, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	nearestID, dist := s.findNearestDriverLocked(pickup, 3)
	if nearestID == "" {
		return Ride{}, errors.New("no nearby drivers available")
	}

	ride := Ride{
		ID:          fmt.Sprintf("ride_%d", time.Now().UnixNano()),
		PassengerID: passengerID,
		DriverID:    nearestID,
		Status:      RideAssigned,
		Pickup:      pickup,
		CreatedAt:   time.Now(),
	}

	driver := s.drivers[nearestID]
	driver.RideID = ride.ID
	driver.Status = "assigned"
	driver.Available = false

	s.drivers[nearestID] = driver
	s.rides[ride.ID] = ride

	if s.persistence != nil {
		if err := s.persistence.SaveRide(ride); err != nil {
			return Ride{}, err
		}
		_ = s.persistence.SetDriverRide(driver.ID, driver.RideID, driver.Status, driver.Available)
	}

	_ = dist // retained for future logging/metrics
	return ride, nil
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
func (s *Store) AcceptRide(rideID, driverID string) (Ride, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ride, ok := s.rides[rideID]
	if !ok {
		return Ride{}, errors.New("ride not found")
	}
	if ride.DriverID != driverID {
		return Ride{}, errors.New("driver mismatch")
	}
	if ride.Status != RideAssigned {
		return Ride{}, errors.New("ride not in assignable state")
	}

	ride.Status = RideAccepted
	s.rides[rideID] = ride

	driver := s.drivers[driverID]
	driver.Status = "accepted"
	driver.Available = false
	driver.RideID = ride.ID
	s.drivers[driverID] = driver

	s.persistRideAndDriver(ride, driver)
	return ride, nil
}

// CancelRide cancels a ride and frees the driver.
func (s *Store) CancelRide(rideID string) (Ride, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ride, ok := s.rides[rideID]
	if !ok {
		return Ride{}, errors.New("ride not found")
	}
	if ride.Status == RideCancelled || ride.Status == RideComplete {
		return Ride{}, errors.New("ride already finished")
	}

	ride.Status = RideCancelled
	s.rides[rideID] = ride

	if ride.DriverID != "" {
		driver := s.drivers[ride.DriverID]
		driver.Status = "idle"
		driver.Available = true
		driver.RideID = ""
		s.drivers[driver.ID] = driver
		s.persistRideAndDriver(ride, driver)
	} else {
		s.persistRideAndDriver(ride, DriverState{})
	}

	return ride, nil
}

// CompleteRide marks a ride complete and frees the driver.
func (s *Store) CompleteRide(rideID string) (Ride, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ride, ok := s.rides[rideID]
	if !ok {
		return Ride{}, errors.New("ride not found")
	}
	if ride.Status != RideAccepted && ride.Status != RideEnRoute {
		return Ride{}, errors.New("ride not in progress")
	}

	ride.Status = RideComplete
	s.rides[rideID] = ride

	if ride.DriverID != "" {
		driver := s.drivers[ride.DriverID]
		driver.Status = "idle"
		driver.Available = true
		driver.RideID = ""
		s.drivers[driver.ID] = driver
		s.persistRideAndDriver(ride, driver)
	} else {
		s.persistRideAndDriver(ride, DriverState{})
	}

	return ride, nil
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
