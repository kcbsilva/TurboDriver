package dispatch

import "time"

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
	ID        string      `json:"id"`
	Available bool        `json:"available"`
	Location  Coordinate  `json:"location"`
	UpdatedAt time.Time   `json:"updatedAt"`
	RideID    string      `json:"rideId,omitempty"`
	Status    string      `json:"status"`
	RadiusKM  float64     `json:"radiusKm"`
}

type Ride struct {
	ID          string     `json:"id"`
	PassengerID string     `json:"passengerId"`
	DriverID    string     `json:"driverId,omitempty"`
	Status      RideStatus `json:"status"`
	Pickup      Coordinate `json:"pickup"`
	CreatedAt   time.Time  `json:"createdAt"`
}
