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

type heartbeatPayload struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Accuracy  float64 `json:"accuracy,omitempty"`
	Timestamp int64   `json:"timestamp"`
}

func main() {
	api := flag.String("api", "http://localhost:8080", "API base URL")
	driverID := flag.String("driver", "sim_driver_1", "driver ID to send heartbeats for")
	token := flag.String("token", "", "bearer token (driver identity)")
	lat := flag.Float64("lat", 40.758, "starting latitude")
	lon := flag.Float64("lon", -73.9855, "starting longitude")
	accuracy := flag.Float64("accuracy", 5, "gps accuracy meters")
	interval := flag.Duration("interval", 3*time.Second, "heartbeat interval")
	count := flag.Int("count", 20, "number of heartbeats to send")
	stepLat := flag.Float64("delta-lat", 0.0001, "increment lat per heartbeat")
	stepLon := flag.Float64("delta-lon", 0.0001, "increment lon per heartbeat")
	flag.Parse()

	client := &http.Client{Timeout: 5 * time.Second}
	for i := 0; i < *count; i++ {
		payload := heartbeatPayload{
			Latitude:  *lat + float64(i)*(*stepLat),
			Longitude: *lon + float64(i)*(*stepLon),
			Accuracy:  *accuracy,
			Timestamp: time.Now().UnixMilli(),
		}
		if err := sendHeartbeat(client, *api, *driverID, *token, payload); err != nil {
			log.Printf("heartbeat %d failed: %v", i+1, err)
		} else {
			log.Printf("heartbeat %d sent", i+1)
		}
		time.Sleep(*interval)
	}
}

func sendHeartbeat(client *http.Client, api, driverID, token string, payload heartbeatPayload) error {
	body, _ := json.Marshal(payload)
	url := fmt.Sprintf("%s/api/drivers/%s/location", api, driverID)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(body))
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
		return fmt.Errorf("status %s", resp.Status)
	}
	return nil
}

func init() {
	log.SetOutput(os.Stdout)
}
