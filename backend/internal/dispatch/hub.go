package dispatch

import (
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

type Hub struct {
	mu         sync.RWMutex
	rideConns  map[string]map[*websocket.Conn]struct{}
	register   chan subscription
	unregister chan subscription
}

type subscription struct {
	rideID string
	conn   *websocket.Conn
}

func NewHub() *Hub {
	return &Hub{
		rideConns:  make(map[string]map[*websocket.Conn]struct{}),
		register:   make(chan subscription),
		unregister: make(chan subscription),
	}
}

func (h *Hub) Run() {
	for {
		select {
		case sub := <-h.register:
			h.mu.Lock()
			if h.rideConns[sub.rideID] == nil {
				h.rideConns[sub.rideID] = make(map[*websocket.Conn]struct{})
			}
			h.rideConns[sub.rideID][sub.conn] = struct{}{}
			h.mu.Unlock()
		case sub := <-h.unregister:
			h.mu.Lock()
			if conns, ok := h.rideConns[sub.rideID]; ok {
				delete(conns, sub.conn)
				if len(conns) == 0 {
					delete(h.rideConns, sub.rideID)
				}
			}
			h.mu.Unlock()
			sub.conn.Close()
		}
	}
}

func (h *Hub) ServeRide(w http.ResponseWriter, r *http.Request, rideID string) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade failed: %v", err)
		return
	}
	h.register <- subscription{rideID: rideID, conn: conn}

	go func() {
		for {
			if _, _, err := conn.NextReader(); err != nil {
				h.unregister <- subscription{rideID: rideID, conn: conn}
				return
			}
		}
	}()
}

func (h *Hub) PublishRideUpdate(ride Ride) {
	h.broadcast(ride.ID, ride)
}

func (h *Hub) PublishDriverUpdate(driverID string, state DriverState) {
	if state.RideID == "" {
		return
	}
	h.broadcast(state.RideID, map[string]any{
		"type":   "driver_location",
		"driver": state,
	})
}

func (h *Hub) broadcast(rideID string, payload any) {
	h.mu.RLock()
	conns := h.rideConns[rideID]
	h.mu.RUnlock()
	for conn := range conns {
		if err := conn.WriteJSON(payload); err != nil {
			h.unregister <- subscription{rideID: rideID, conn: conn}
		}
	}
}
