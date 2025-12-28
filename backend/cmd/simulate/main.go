package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
)

type rideRequest struct {
	PassengerId string  `json:"passengerId"`
	PickupLat   float64 `json:"pickupLat"`
	PickupLong  float64 `json:"pickupLong"`
}

type acceptPayload struct {
	DriverId string `json:"driverId"`
}

func main() {
	api := flag.String("api", "http://localhost:8080", "API base URL")
	passengerToken := flag.String("passenger-token", "", "passenger bearer token")
	driverToken := flag.String("driver-token", "", "driver bearer token")
	passengerID := flag.String("passenger-id", "sim_passenger_1", "passenger id (if token role not enforced)")
	driverID := flag.String("driver-id", "sim_driver_1", "driver id")
	lat := flag.Float64("lat", 40.758, "pickup latitude")
	lon := flag.Float64("lon", -73.9855, "pickup longitude")
	flag.Parse()

	client := &http.Client{Timeout: 5 * time.Second}

	rideID, err := requestRide(client, *api, *passengerToken, rideRequest{
		PassengerId: *passengerID,
		PickupLat:   *lat,
		PickupLong:  *lon,
	})
	if err != nil {
		log.Fatalf("ride request failed: %v", err)
	}
	log.Printf("ride requested: %s", rideID)

	if err := acceptRide(client, *api, *driverToken, rideID, *driverID); err != nil {
		log.Fatalf("accept failed: %v", err)
	}
	log.Printf("ride accepted by %s", *driverID)
}

func requestRide(client *http.Client, api, token string, payload rideRequest) (string, error) {
	body, _ := json.Marshal(payload)
	req, err := http.NewRequest("POST", fmt.Sprintf("%s/api/rides", api), bytes.NewBuffer(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("request ride status: %s", resp.Status)
	}
	var res map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", err
	}
	if id, ok := res["id"].(string); ok {
		return id, nil
	}
	return "", fmt.Errorf("ride id missing in response")
}

func acceptRide(client *http.Client, api, token, rideID, driverID string) error {
	body, _ := json.Marshal(acceptPayload{DriverId: driverID})
	req, err := http.NewRequest("POST", fmt.Sprintf("%s/api/rides/%s/accept", api, rideID), bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("accept status: %s", resp.Status)
	}
	return nil
}

func init() {
	log.SetOutput(os.Stdout)
}
