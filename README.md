# TurboDriver

Ride-Hailing App – Technical Summary

Single-City MVP (Google Maps Stack)

Objective

Build a single-city ride-hailing MVP (Uber-like) with a lean, controlled scope, prioritizing real-time reliability, predictable costs, and fast iteration. This is not a multi-city or surge-pricing platform in Phase 1.

Platform Scope
Mobile Apps

Passenger App (iOS + Android)

Driver App (iOS + Android)

Shared codebase using React Native

City Scope

One city only

Fixed pricing (no surge)

One active ride per driver

Radius-based driver matching

Technology Decisions (Locked)
Mobile

React Native

Google Maps SDK (native, not WebView)

Native GPS access (foreground + background)

WebSockets for real-time updates

Push notifications (APNs / FCM)

Rationale:
Google Maps SDK provides native performance, reliable routing, traffic-aware ETA, and avoids WebView complexity. It is the industry standard for ride-hailing.

Maps & Routing

Google Maps SDK (rendering)

Google Directions API (routes)

Google Distance Matrix API (ETA)

Optional: Google Roads API (GPS snapping)

Maps are used for visualization + routing, while dispatch logic remains backend-driven.

Backend Architecture

Go (primary backend language)

WebSockets (real-time location & state sync)

REST APIs (auth, rides, payments, admin)

Redis

Geo queries (nearby drivers)

Ride/driver state

PostgreSQL

Users

Drivers

Rides

Payments

Fully Dockerized local development

Real-Time Flow (Authoritative)

Driver app sends GPS updates every 2–5 seconds

Backend validates + stores coordinates

Redis geo-index updated

Dispatch engine selects candidate drivers

Passenger app subscribes via WebSocket

Google Maps updates markers + route polylines

Important:
Google Maps renders data — the backend decides everything.

Dispatch Logic (MVP)

Nearest-driver matching within radius

FIFO or distance-based ordering

Single acceptance window

Timeout → reassignment

Manual admin override acceptable

No surge pricing or advanced optimization in Phase 1.

Development Environment
IDE

Visual Studio Code (primary editor)

iOS

macOS required

Xcode required only for:

iOS Simulator

Signing & provisioning

TestFlight & App Store

Real iPhones available for physical GPS testing

Android

Android Emulator (no physical device required)

Android Studio used only for:

SDK

Emulator

Emulator supports GPS route simulation

Costs (Confirmed & Expected)
Apple (iOS)

Apple Developer Program: USD 99 / year

No per-app or per-device fees

Ride payments = physical service → no Apple commission

Google Maps API (Estimated)

Maps SDK usage is mostly free under credits

Directions / Distance Matrix incur usage-based costs

Single-city MVP stays within low monthly cost

Billing account required

Explicitly Out of Scope (MVP)

Multi-city support

Surge pricing

Driver ranking algorithms

ML-based dispatch

Advanced fraud detection

Multiple vehicle classes

Engineering Principles

Backend-first authority

Deterministic state machines

Real-time correctness > features

Cost-controlled APIs

Replaceable components in later phases

Next Technical Deliverables

Repository structure

Driver vs passenger app separation

WebSocket message schemas

Dispatch state machine

Google Maps integration patterns (markers, polylines)

Docker Compose for backend

Final Note to Team

This scope is intentional and realistic.
Anything resembling “full Uber parity” is explicitly Phase 2+.

## Repo Layout

- `backend/` – Go service exposing REST + WebSocket for rides/driver updates (Dockerized).
- `docker-compose.yml` – local stack with API + PostgreSQL + Redis (data services ready for future persistence).

## Backend Quickstart

```bash
docker-compose up --build
```

The API listens on `http://localhost:8080`.
- Persistence: API uses `DATABASE_URL` (set in Compose to Postgres) and auto-creates minimal `drivers`/`rides` tables. If `DATABASE_URL` is unset or DB is unavailable, it falls back to in-memory state.
- Geo: API uses `REDIS_URL` (set in Compose) for GEO-based nearest-driver search; falls back to in-memory if unavailable.
- Auth: In-memory token issuance (dev mode). Set `AUTH_MODE=memory` (default in docker-compose).
  - `POST /api/auth/register` with body `{"role":"driver"|"passenger"|"admin"}` issues an ID and token.
  - All `/api/*` endpoints require `Authorization: Bearer <token>` once auth is enabled; `/ws/rides/{rideID}` accepts header or `?token=` query param.
  - Role enforcement: drivers may send locations/accept/complete; passengers may request rides/cancel; admins bypass checks and can register new identities.
- Identity persistence: when Postgres is available, identities are stored in `identities` table and read alongside in-memory cache (auth tokens survive restarts).
  - Tokens default to 30d TTL (`AUTH_TTL`, e.g. `24h`), stored in DB with expiry and skipped if expired when seeding the cache.
  - Ride events are stored in `ride_events` (if Postgres is enabled) and exposed via the admin endpoint.
  - Ride history endpoints: `/api/history/passenger` and `/api/history/driver` (role-scoped).

### Seeding Identities (dev)

Sample seed script to create passenger/driver/admin tokens in Postgres:
```bash
cd backend
go run ./cmd/seed
```
Use `DATABASE_URL` env to point at your DB (defaults to docker-compose Postgres).

### Simulate Driver Heartbeats (dev)

Send a series of driver location updates to the API:
```bash
cd backend
go run ./cmd/heartbeat --driver=sim_driver_1 --token=DRIVER_TOKEN --lat=40.758 --lon=-73.9855 --count=20 --interval=3s
```
Flags: `--api` (default http://localhost:8080), `--accuracy`, `--delta-lat`, `--delta-lon` to move per tick.

### Simulate Ride Request + Accept (dev)

Trigger a ride request and auto-accept with a driver:
```bash
cd backend
go run ./cmd/simulate --passenger-token=PASS_TOKEN --driver-token=DRIVER_TOKEN --driver-id=sim_driver_1 --lat=40.758 --lon=-73.9855
```

### Admin Viewer (static)

`backend/static/admin/index.html` is a minimal viewer to query rides and events. Open in browser, set API base/token, and fetch ride/events.
Or run a local server:
```bash
cd backend
go run ./cmd/serve-admin --addr=:8090
```

### HTTP & WebSocket Surface (MVP)

- `GET /health` – readiness probe.
- `POST /api/drivers/{driverID}/location` – driver GPS heartbeat (2–5s). Body: `{"latitude":..., "longitude":..., "accuracy":optional, "timestamp":optional_ms}`. Marks driver available unless on a ride; broadcasts to ride subscribers.
- `POST /api/rides` – passenger ride request. Body: `{"passengerId":"p1","pickupLat":..., "pickupLong":...}`. Matches nearest available driver within 3km, sets status `assigned`, and broadcasts on the ride channel.
- `GET /api/rides/{rideID}` – fetch ride snapshot.
- `POST /api/rides/{rideID}/accept` – driver accepts ride. Body: `{"driverId":"d1"}`. Moves ride to `accepted`.
- `POST /api/rides/{rideID}/cancel` – cancel ride (passenger/admin flow). Frees driver.
- `POST /api/rides/{rideID}/complete` – mark ride complete. Frees driver.
- `GET /ws/rides/{rideID}` – subscribe to ride + driver updates (server pushes JSON frames).
  - Acceptance window: ~15 seconds. If a ride stays `assigned` without acceptance, it frees the driver and tries to reassign another nearby driver; if none are found, the ride reverts to `requested`.
  - `GET /api/history/passenger` – authenticated passenger ride history (passenger only).
  - `GET /api/history/driver` – authenticated driver ride history (driver only).
- `GET /api/admin/rides/{rideID}/events` – admin-only audit log of ride events.
  - History/events return `{data, limit, offset, total}` for pagination.

### Matching Rules (current)

- Single-city, radius-based nearest-driver selection (3 km), FIFO by proximity.
- Drivers marked busy once assigned. One ride per driver.
- Redis GEO used for nearest-driver lookup when available; falls back to in-memory search.
- In-memory state for now; Postgres/Redis are in Compose to align with the target stack and future persistence.

## Next Steps

- Extend persistence to all ride transitions (and historical querying) and thread Redis into geo queries.
- Add auth + user/driver registration endpoints.
- Stand up React Native Passenger/Driver apps using Google Maps SDK (native) and connect to WebSocket channels.
- Expand ride state machine (accept/timeout/cancel/complete) and admin override path.
