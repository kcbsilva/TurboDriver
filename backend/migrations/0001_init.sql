-- Initial schema migration
CREATE TABLE IF NOT EXISTS drivers (
    id TEXT PRIMARY KEY,
    latitude DOUBLE PRECISION NOT NULL,
    longitude DOUBLE PRECISION NOT NULL,
    accuracy DOUBLE PRECISION,
    ts TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL,
    ride_id TEXT,
    radius_km DOUBLE PRECISION NOT NULL DEFAULT 3,
    available BOOLEAN NOT NULL DEFAULT TRUE,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS rides (
    id TEXT PRIMARY KEY,
    passenger_id TEXT NOT NULL,
    driver_id TEXT,
    status TEXT NOT NULL,
    pickup_lat DOUBLE PRECISION NOT NULL,
    pickup_long DOUBLE PRECISION NOT NULL,
    pickup_accuracy DOUBLE PRECISION,
    pickup_ts TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS ride_events (
    id BIGSERIAL PRIMARY KEY,
    ride_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    payload JSONB,
    actor_id TEXT,
    actor_role TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS ride_events_ride_id_idx ON ride_events(ride_id, created_at);

CREATE TABLE IF NOT EXISTS identities (
    id TEXT PRIMARY KEY,
    role TEXT NOT NULL,
    token TEXT UNIQUE NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ
);
