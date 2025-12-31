package dispatch

import (
	"context"
	"time"
)

type RideStatus string

const (
	RideRequested RideStatus = "requested"
	RideAssigned  RideStatus = "assigned"
	RideAccepted  RideStatus = "accepted"
	RideEnRoute   RideStatus = "en_route"
	RideComplete  RideStatus = "complete"
	RideCancelled RideStatus = "cancelled"
)

type Coordinate struct {
	Latitude  float64   `json:"latitude"`
	Longitude float64   `json:"longitude"`
	Accuracy  float64   `json:"accuracy,omitempty"`
	At        time.Time `json:"timestamp"`
}

type DriverState struct {
	ID        string     `json:"id"`
	Available bool       `json:"available"`
	Location  Coordinate `json:"location"`
	UpdatedAt time.Time  `json:"updatedAt"`
	RideID    string     `json:"rideId,omitempty"`
	Status    string     `json:"status"`
	RadiusKM  float64    `json:"radiusKm"`
}

type IdentityRole string

const (
	RolePassenger IdentityRole = "passenger"
	RoleDriver    IdentityRole = "driver"
	RoleAdmin     IdentityRole = "admin"
)

type Identity struct {
	ID    string       `json:"id"`
	Role  IdentityRole `json:"role"`
	Token string       `json:"token,omitempty"`
	// ExpiresAt is optional; nil means no expiry.
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

type Ride struct {
	ID          string     `json:"id"`
	PassengerID string     `json:"passengerId"`
	DriverID    string     `json:"driverId,omitempty"`
	Status      RideStatus `json:"status"`
	Pickup      Coordinate `json:"pickup"`
	CreatedAt   time.Time  `json:"createdAt"`
}

type RideEvent struct {
	RideID    string    `json:"rideId"`
	Type      string    `json:"type"`
	Payload   []byte    `json:"payload,omitempty"`
	ActorID   string    `json:"actorId,omitempty"`
	ActorRole string    `json:"actorRole,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

type EventLogger interface {
	AppendRideEvent(ctx context.Context, evt RideEvent) error
	ListRideEvents(ctx context.Context, rideID string, limit, offset int) ([]RideEvent, error)
	CountRideEvents(ctx context.Context, rideID string) (int, error)
}

type RideTransaction interface {
	CreateRideWithEvent(ctx context.Context, ride Ride, event RideEvent, driver DriverState) error
	UpdateRideWithEvent(ctx context.Context, ride Ride, event RideEvent, driver *DriverState) error
}

type IdempotencyStore interface {
	Remember(ctx context.Context, key, rideID string) error
	Lookup(ctx context.Context, key string) (string, bool, error)
}

// RideLister provides ride history for identities.
type RideLister interface {
	ListRidesByPassenger(ctx context.Context, passengerID string, limit, offset int) ([]Ride, error)
	ListRidesByDriver(ctx context.Context, driverID string, limit, offset int) ([]Ride, error)
	CountRidesByPassenger(ctx context.Context, passengerID string) (int, error)
	CountRidesByDriver(ctx context.Context, driverID string) (int, error)
}

// Driver application domain

type DriverApplicationStatus string

const (
	ApplicationPending     DriverApplicationStatus = "pending"
	ApplicationApproved    DriverApplicationStatus = "approved"
	ApplicationRejected    DriverApplicationStatus = "rejected"
	ApplicationNeedsReview DriverApplicationStatus = "needs_review"
)

type LocationRule struct {
	ID           int64     `json:"id"`
	LocationCode string    `json:"locationCode"`
	Name         string    `json:"name"`
	Rules        []byte    `json:"rules"` // JSON blob
	EffectiveAt  time.Time `json:"effectiveAt"`
	CreatedAt    time.Time `json:"createdAt"`
}

type DriverApplication struct {
	ID           int64                   `json:"id"`
	DriverID     string                  `json:"driverId"`
	LocationCode string                  `json:"locationCode"`
	RulesVersion *int64                  `json:"rulesVersion,omitempty"`
	Status       DriverApplicationStatus `json:"status"`
	License      DriverLicense           `json:"license"`
	Vehicle      DriverVehicle           `json:"vehicle"`
	Photos       []VehiclePhoto          `json:"photos,omitempty"`
	Liveness     DriverLiveness          `json:"liveness"`
	CreatedAt    time.Time               `json:"createdAt"`
	UpdatedAt    time.Time               `json:"updatedAt"`
}

type DriverLicense struct {
	ID          int64      `json:"id"`
	DriverID    string     `json:"driverId"`
	Number      string     `json:"number"`
	Country     string     `json:"country,omitempty"`
	Region      string     `json:"region,omitempty"`
	ExpiresAt   *time.Time `json:"expiresAt,omitempty"`
	Remunerated bool       `json:"remunerated"`
	DocumentURL string     `json:"documentUrl,omitempty"`
	VerifiedAt  *time.Time `json:"verifiedAt,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

type DriverVehicle struct {
	ID              int64      `json:"id"`
	DriverID        string     `json:"driverId"`
	Type            string     `json:"type"` // car | motorcycle | bus
	PlateNumber     string     `json:"plateNumber,omitempty"`
	DocumentNumber  string     `json:"documentNumber,omitempty"`
	DocumentURL     string     `json:"documentUrl,omitempty"`
	DocumentExpires *time.Time `json:"documentExpiresAt,omitempty"`
	Ownership       string     `json:"ownership"` // owns | renting | lent
	ContractURL     string     `json:"contractUrl,omitempty"`
	ContractExpires *time.Time `json:"contractExpiresAt,omitempty"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
}

type VehiclePhoto struct {
	ID        int64     `json:"id"`
	VehicleID int64     `json:"vehicleId"`
	Angle     string    `json:"angle"` // front | back | left | right
	PhotoURL  string    `json:"photoUrl"`
	CreatedAt time.Time `json:"createdAt"`
}

type DriverLiveness struct {
	ID                int64      `json:"id"`
	DriverID          string     `json:"driverId"`
	ChallengeSequence []string   `json:"challengeSequence"`
	Captures          []byte     `json:"captures"` // JSON map direction -> photo URL
	Verified          bool       `json:"verified"`
	VerifiedAt        *time.Time `json:"verifiedAt,omitempty"`
	CreatedAt         time.Time  `json:"createdAt"`
}

// Passenger profile
type PassengerProfile struct {
	ID           int64     `json:"id"`
	PassengerID  string    `json:"passengerId"`
	FullName     string    `json:"fullName"`
	Address      string    `json:"address,omitempty"`
	GovernmentID string    `json:"governmentId,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type Rating struct {
	ID                int64        `json:"id"`
	RideID            string       `json:"rideId"`
	RaterRole         IdentityRole `json:"raterRole"` // driver or passenger
	RaterID           string       `json:"raterId"`
	RateeID           string       `json:"rateeId"`
	Stars             int          `json:"stars"` // 1-5
	Comment           string       `json:"comment,omitempty"`
	RequiresAttention bool         `json:"requiresAttention"`
	CreatedAt         time.Time    `json:"createdAt"`
}
