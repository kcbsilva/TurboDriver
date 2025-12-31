# TurboDriver Mobile Sandbox

Minimal React Native client to exercise the backend (heartbeat, request ride, accept, and WS updates).

## Prereqs

- Node 18+
- Xcode (for iOS simulator) and/or Android Studio SDKs
- React Native CLI (`npx react-native --version` works)

## Install

```bash
cd mobile
npm install
```

- iOS native project is included under `ios/`. After installing deps, run:
```bash
cd ios && pod install
cd ..
```
- Android can be scaffolded later if needed.

## Run (iOS simulator)

```bash
cd mobile
npx react-native start        # Terminal 1 (Metro)
npx react-native run-ios --simulator="iPhone 15"   # Terminal 2
```

## Configure and Test Flow

1) Generate tokens from the backend:
```bash
cd backend
ALLOW_SIGNUP=true go run ./cmd/seed   # or POST /api/auth/signup per README
```
2) In the app UI set:
- API Base: `http://localhost:8080`
- WS Base: `ws://localhost:8080`
- Passenger Token: token from seed/signup
- Driver Token: token from seed/signup
3) Tap **Heartbeat** (populates driver), **Request Ride** (creates ride + WS subscribe), then **Accept Ride**.

Log output appears in-app; backend logs/metrics confirm events.
