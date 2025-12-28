package geo

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

// Index wraps a Redis GEO index for drivers.
type Index struct {
	client *redis.Client
	key    string
}

func NewIndex(client *redis.Client) *Index {
	return &Index{client: client, key: "drivers:geo"}
}

// AddDriver stores/updates driver coordinates.
func (i *Index) AddDriver(ctx context.Context, driverID string, lat, lon float64) error {
	return i.client.GeoAdd(ctx, i.key, &redis.GeoLocation{
		Name:      driverID,
		Longitude: lon,
		Latitude:  lat,
	}).Err()
}

// RemoveDriver removes a driver from the geo index.
func (i *Index) RemoveDriver(ctx context.Context, driverID string) error {
	return i.client.ZRem(ctx, i.key, driverID).Err()
}

// PruneOlderThan is a no-op for Redis GEO; rely on heartbeat TTL in Store.
func (i *Index) PruneOlderThan(cutoff time.Time) {}

// Nearby finds the nearest driver within radius km.
func (i *Index) Nearby(ctx context.Context, lat, lon, radiusKM float64) (string, float64, error) {
	results, err := i.client.GeoSearchLocation(ctx, i.key, &redis.GeoSearchLocationQuery{
		GeoSearchQuery: redis.GeoSearchQuery{
			Longitude:  lon,
			Latitude:   lat,
			Radius:     radiusKM,
			RadiusUnit: "km",
			Sort:       "ASC",
			Count:      1,
		},
		WithDist: true,
	}).Result()
	if err != nil {
		return "", 0, err
	}
	if len(results) == 0 {
		return "", 0, errors.New("no drivers in radius")
	}
	return results[0].Name, results[0].Dist, nil
}
