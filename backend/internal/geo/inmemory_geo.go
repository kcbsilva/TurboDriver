package geo

import (
	"errors"
	"math"
	"sync"
	"time"
)

// InMemoryGeo provides a simple fallback geo index.
type InMemoryGeo struct {
	mu       sync.RWMutex
	coords   map[string][2]float64
	haversin func(lat1, lon1, lat2, lon2 float64) float64
}

func NewInMemoryGeo() *InMemoryGeo {
	return &InMemoryGeo{
		coords: make(map[string][2]float64),
		haversin: func(lat1, lon1, lat2, lon2 float64) float64 {
			const earthRadiusKM = 6371
			dLat := toRadians(lat2 - lat1)
			dLon := toRadians(lon2 - lon1)
			lat1Rad := toRadians(lat1)
			lat2Rad := toRadians(lat2)
			sinLat := math.Sin(dLat / 2)
			sinLon := math.Sin(dLon / 2)
			calc := sinLat*sinLat + math.Cos(lat1Rad)*math.Cos(lat2Rad)*sinLon*sinLon
			return 2 * earthRadiusKM * math.Asin(math.Sqrt(calc))
		},
	}
}

func (g *InMemoryGeo) Add(driverID string, lat, lon float64) error {
	g.mu.Lock()
	g.coords[driverID] = [2]float64{lat, lon}
	g.mu.Unlock()
	return nil
}

func (g *InMemoryGeo) Remove(driverID string) error {
	g.mu.Lock()
	delete(g.coords, driverID)
	g.mu.Unlock()
	return nil
}

func (g *InMemoryGeo) PruneOlderThan(cutoff time.Time) {
	// in-memory geo lacks timestamps; no-op
}

func (g *InMemoryGeo) Nearby(lat, lon, radiusKM float64) (string, float64, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	bestID := ""
	bestDist := math.MaxFloat64
	for id, pt := range g.coords {
		dist := g.haversin(lat, lon, pt[0], pt[1])
		if dist <= radiusKM && dist < bestDist {
			bestID = id
			bestDist = dist
		}
	}
	if bestID == "" {
		return "", 0, errors.New("no drivers in radius")
	}
	return bestID, bestDist, nil
}

func toRadians(deg float64) float64 {
	return deg * math.Pi / 180
}
