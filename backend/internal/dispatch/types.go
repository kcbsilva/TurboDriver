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

// RideLister provides ride history for identities.
type RideLister interface {
	ListRidesByPassenger(ctx context.Context, passengerID string, limit, offset int) ([]Ride, error)
	ListRidesByDriver(ctx context.Context, driverID string, limit, offset int) ([]Ride, error)
}
