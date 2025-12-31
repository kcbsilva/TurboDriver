package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"time"

	"github.com/gorilla/websocket"
)

func main() {
	api := envOrDefault("API_BASE", "http://localhost:8080")
	wsBase := envOrDefault("WS_BASE", "ws://localhost:8080")

	// seed identities
	fmt.Println("Seeding identities...")
	if err := runCmd("go", "run", "./cmd/seed"); err != nil {
		log.Fatalf("seed failed: %v", err)
	}

	passToken := envOrDefault("PASSENGER_TOKEN", "")
	driverToken := envOrDefault("DRIVER_TOKEN", "")
	if passToken == "" || driverToken == "" {
		fmt.Println("Fetch tokens from seed output (passenger/driver) and set PASSENGER_TOKEN/DRIVER_TOKEN env for non-interactive run.")
	}

	// simulate heartbeat (one tick)
	fmt.Println("Sending driver heartbeat...")
	hbPayload := map[string]any{
		"latitude":  40.758,
		"longitude": -73.9855,
		"accuracy":  5,
		"timestamp": time.Now().UnixMilli(),
	}
	if err := postJSON(api+"/api/drivers/sim_driver_1/location", driverToken, hbPayload); err != nil {
		log.Fatalf("heartbeat failed: %v", err)
	}

	// request ride
	fmt.Println("Requesting ride...")
	rideID, err := requestRide(api, passToken, map[string]any{
		"pickupLat":      40.758,
		"pickupLong":     -73.9855,
		"idempotencyKey": fmt.Sprintf("smoke-%d", time.Now().UnixNano()),
	})
	if err != nil {
		log.Fatalf("request ride failed: %v", err)
	}
	fmt.Printf("Ride ID: %s\n", rideID)

	// subscribe to WS
	events := make(chan map[string]any, 5)
	go subscribeWS(wsBase, rideID, passToken, events)

	// accept ride
	fmt.Println("Accepting ride...")
	if err := postJSON(fmt.Sprintf("%s/api/rides/%s/accept", api, rideID), driverToken, map[string]any{
		"driverId": "sim_driver_1",
	}); err != nil {
		log.Fatalf("accept failed: %v", err)
	}

	waitForStatus(events, "accepted", rideID)

	fmt.Println("Smoke test complete.")
}

func requestRide(api, token string, payload map[string]any) (string, error) {
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", api+"/api/rides", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("status %s", resp.Status)
	}
	var res map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", err
	}
	idVal, ok := res["id"]
	if !ok || idVal == nil {
		return "", fmt.Errorf("ride id missing")
	}
	id, _ := idVal.(string)
	if id == "" {
		return "", fmt.Errorf("ride id missing")
	}
	return id, nil
}

func postJSON(url, token string, payload map[string]any) error {
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", url, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("status %s", resp.Status)
	}
	return nil
}

func runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), "DATABASE_URL="+envOrDefault("DATABASE_URL", ""))
	return cmd.Run()
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func subscribeWS(base, rideID, token string, sink chan<- map[string]any) {
	u := fmt.Sprintf("%s/ws/rides/%s", base, rideID)
	parsed, _ := url.Parse(u)
	q := parsed.Query()
	if token != "" {
		q.Set("token", token)
	}
	parsed.RawQuery = q.Encode()

	c, _, err := websocket.DefaultDialer.Dial(parsed.String(), nil)
	if err != nil {
		log.Printf("ws dial failed: %v", err)
		return
	}
	defer c.Close()
	for {
		_, msg, err := c.ReadMessage()
		if err != nil {
			return
		}
		var payload map[string]any
		if err := json.Unmarshal(msg, &payload); err != nil {
			continue
		}
		sink <- payload
	}
}

func waitForStatus(events <-chan map[string]any, expect, rideID string) {
	timeout := time.After(8 * time.Second)
	for {
		select {
		case msg := <-events:
			status, _ := msg["status"].(string)
			if status == "" {
				continue
			}
			if id, ok := msg["id"].(string); ok && id != "" && rideID != "" && id != rideID {
				continue
			}
			if driver, ok := msg["driverId"].(string); ok && driver == "" {
				log.Fatalf("ws payload missing driverId: %v", msg)
			}
			fmt.Printf("WS update received: %v\n", msg)
			if status == expect {
				return
			}
		case <-timeout:
			log.Fatalf("expected ws status %q not received", expect)
		}
	}
}
