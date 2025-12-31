-- TurboDriver database schema

-- Drivers: latest driver state (replace with more granular history later)
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

-- Rides: primary ride record
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

-- Ride events: append-only audit log
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

-- Identities: issued tokens with optional expiry
CREATE TABLE IF NOT EXISTS identities (
    id TEXT PRIMARY KEY,
    role TEXT NOT NULL,
    token TEXT UNIQUE NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ
);

-- Passenger profile info
CREATE TABLE IF NOT EXISTS passenger_profiles (
    id BIGSERIAL PRIMARY KEY,
    passenger_id TEXT NOT NULL REFERENCES identities(id) ON DELETE CASCADE,
    full_name TEXT NOT NULL,
    address TEXT,
    government_id TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS passenger_profiles_passenger_idx ON passenger_profiles(passenger_id);

-- Location-specific rules (per city/region) that drivers accept during signup
CREATE TABLE IF NOT EXISTS location_rules (
    id BIGSERIAL PRIMARY KEY,
    location_code TEXT NOT NULL, -- e.g., city or region code
    name TEXT NOT NULL,
    rules JSONB NOT NULL, -- structured rule payload for that location
    effective_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS location_rules_code_idx ON location_rules(location_code, effective_at DESC);

-- Driver applications capture signup state per driver and location
CREATE TABLE IF NOT EXISTS driver_applications (
    id BIGSERIAL PRIMARY KEY,
    driver_id TEXT NOT NULL REFERENCES identities(id) ON DELETE CASCADE,
    location_code TEXT NOT NULL,
    rules_version_id BIGINT REFERENCES location_rules(id),
    status TEXT NOT NULL DEFAULT 'pending', -- pending, approved, rejected, needs_review
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS driver_applications_driver_idx ON driver_applications(driver_id);
CREATE INDEX IF NOT EXISTS driver_applications_driver_idx ON driver_applications(driver_id);

-- Driver license details with remunerated-permission flag
CREATE TABLE IF NOT EXISTS driver_licenses (
    id BIGSERIAL PRIMARY KEY,
    driver_id TEXT NOT NULL REFERENCES identities(id) ON DELETE CASCADE,
    license_number TEXT NOT NULL,
    country TEXT,
    region TEXT,
    expires_at TIMESTAMPTZ,
    remunerated BOOLEAN NOT NULL DEFAULT FALSE,
    document_url TEXT, -- stored blob/object link
    verified_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS driver_licenses_driver_idx ON driver_licenses(driver_id);

-- Vehicles tied to drivers
CREATE TABLE IF NOT EXISTS driver_vehicles (
    id BIGSERIAL PRIMARY KEY,
    driver_id TEXT NOT NULL REFERENCES identities(id) ON DELETE CASCADE,
    vehicle_type TEXT NOT NULL, -- car | motorcycle | bus
    plate_number TEXT,
    document_number TEXT,
    document_expires_at TIMESTAMPTZ,
    ownership TEXT NOT NULL, -- owns | renting | lent
    contract_url TEXT, -- required when renting or lent
    contract_expires_at TIMESTAMPTZ,
    document_url TEXT, -- registration / circulation document
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS driver_vehicles_driver_idx ON driver_vehicles(driver_id);

-- Vehicle photos (front/back/left/right)
CREATE TABLE IF NOT EXISTS vehicle_photos (
    id BIGSERIAL PRIMARY KEY,
    vehicle_id BIGINT NOT NULL REFERENCES driver_vehicles(id) ON DELETE CASCADE,
    angle TEXT NOT NULL, -- front | back | left | right
    photo_url TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS vehicle_photos_vehicle_idx ON vehicle_photos(vehicle_id);

-- Driver liveness/selfie checks with directional prompts
CREATE TABLE IF NOT EXISTS driver_liveness_checks (
    id BIGSERIAL PRIMARY KEY,
    driver_id TEXT NOT NULL REFERENCES identities(id) ON DELETE CASCADE,
    challenge_sequence JSONB NOT NULL, -- e.g., ["up","left","right","down"]
    captures JSONB NOT NULL, -- map of direction -> photo url
    verified BOOLEAN NOT NULL DEFAULT FALSE,
    verified_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS driver_liveness_driver_idx ON driver_liveness_checks(driver_id);

-- Ride ratings (one per role per ride)
CREATE TABLE IF NOT EXISTS ride_ratings (
    id BIGSERIAL PRIMARY KEY,
    ride_id TEXT NOT NULL,
    rater_role TEXT NOT NULL, -- driver | passenger
    rater_id TEXT NOT NULL,
    ratee_id TEXT NOT NULL,
    stars INT NOT NULL CHECK (stars >= 1 AND stars <= 5),
    comment TEXT,
    requires_attention BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS ride_ratings_unique_role ON ride_ratings(ride_id, rater_role);
