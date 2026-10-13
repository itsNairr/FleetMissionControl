# Rivian Mission Control HMI

[![Go](https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat&logo=go)](https://golang.org)
[![React](https://img.shields.io/badge/React-18-61DAFB?style=flat&logo=react)](https://react.dev)
[![TypeScript](https://img.shields.io/badge/TypeScript-5.0+-3178C6?style=flat&logo=typescript)](https://www.typescriptlang.org)
[![Protocol Buffers](https://img.shields.io/badge/Protobuf-v3-4285F4?style=flat&logo=google)](https://protobuf.dev)
[![Three.js](https://img.shields.io/badge/Three.js-WebGL-black?style=flat&logo=three.js)](https://threejs.org)
[![Vite](https://img.shields.io/badge/Vite-6.x-646CFF?style=flat&logo=vite)](https://vitejs.dev)

Fleet Mission Control is a real-time connected vehicle telemetry and remote operations platform built with Go, React, and Three.js.

Modern electric vehicles stream thousands of sensor updates every second, from battery pack temperatures and inverter currents to tire pressures and GPS coordinates. At the same time, fleet operators need to send commands back to vehicles safely, securely, and with sub-second feedback.

I built this project to model that full lifecycle end to end:
1. **Telemetry Ingestion:** Cars stream binary Protocol Buffer frames over persistent TCP connections into a concurrent Go worker pool and in-memory digital twin.
2. **Cryptographic Dispatch:** The cloud gateway signs outbound commands with ECDSA keys, and the vehicle ECU validates timestamps and nonces before execution.
3. **Automotive Safety:** An onboard safety layer enforces ISO-26262 interlocks, rejecting dangerous commands like opening the frunk or tailgate while driving.
4. **Mission Control Dashboard:** A clean operations interface with real-time California road tracking, live alerts, and an interactive 3D digital twin inspired by Rivian OS.

---

## Architecture

```
                      ┌──────────────────────────────────────────────┐
                      │          Vehicle Fleet Simulator             │
                      │  • 14 Real-world EV Archetypes               │
                      │  • California OSRM/OSM Road Following        │
                      │  • ISO-26262 Functional Safety Interlocks    │
                      └───────┬──────────────────────────────▲───────┘
                              │                              │
         [Telemetry Stream]   │ Length-Prefixed TCP Stream   │ [Command Highway]
         Binary Protobuf      │ [len][type][payload]         │ ECDSA Signed Commands
                              ▼                              │
                      ┌──────────────────────────────────────┴───────┐
                      │                Gateway (:8080)               │
                      │  • Concurrent TCP Session Registry           │
                      │  • Thread-Safe Frame Reader / Writer         │
                      │  • ECDSA SHA-256 Command Signer              │
                      └───────┬──────────────────────────────▲───────┘
                              │                              │
                              ▼                              │
                      ┌──────────────────────┐               │
                      │ TelemetryWorkerPool  │               │
                      │ • Channel queue      │               │
                      │ • Rules engine       │               │
                      │ • Digital Twin cache │               │
                      └───────┬──────────────┘               │
                              │                              │
                              ▼                              │
                      ┌──────────────────────────────────────┴───────┐
                      │          HTTP & WebSocket API (:8081)        │
                      │  • WS /api/v1/fleet/stream                   │
                      │  • WS /api/v1/alerts/stream                  │
                      │  • POST /api/v1/vehicles/{vin}/command       │
                      └───────┬──────────────────────────────▲───────┘
                              │                              │
                              ▼                              │
                      ┌──────────────────────────────────────┴───────┐
                      │             Mission Control HMI              │
                      │  • Live Leaflet Road Fleet Map               │
                      │  • Interactive 3D WebGL Digital Twin         │
                      │  • Rivian OS Telemetry & Arc Gauges          │
                      │  • Optimistic Remote Actuation Hotspots      │
                      └──────────────────────────────────────────────┘
```

---

## How It Works Under the Hood

### 1. Binary Wire Framing over Raw TCP
Instead of sending bulky JSON payloads over HTTP, vehicles maintain a persistent TCP socket to the gateway.

To prevent TCP stream fragmentation and packet coalescing under load, every frame uses a compact 5-byte header:
* `[4 bytes]`: Big-endian payload length
* `[1 byte]`: Message type (`0x01` for Telemetry, `0x02` for Command, `0x03` for Command Ack)
* `[N bytes]`: Google Protocol Buffers binary payload

This keeps serialization overhead minimal and lets a single socket handle telemetry streaming, cloud commands, and acknowledgments simultaneously.

### 2. Concurrent Worker Pool and Thread-Safe Digital Twins
* **Worker Pool:** Ingestion runs through an actor-style worker pool with bounded Go channels. If a spike in traffic occurs, non-blocking select statements shed backpressure safely instead of overwhelming the server.
* **In-Memory Digital Twin:** A thread-safe state registry guarded by `sync.RWMutex` caches the latest snapshot for every vehicle, giving the WebSocket server zero-allocation read access.
* **Live Rules Engine:** As telemetry arrives, a rules engine checks for anomalies on every tick:
  * Battery thermal runaway (pack temperature exceeding 55°C)
  * Voltage imbalance (cell delta greater than 0.1V)
  * Speed governor violations and low tire pressure (TPMS) alerts

### 3. Cryptographic Commands and ISO-26262 Safety
* **ECDSA Command Signatures:** When an operator clicks a button, the gateway signs the payload using an ECDSA private key (`secp256r1` with SHA-256). The vehicle verifies the signature against the server's public key before touching hardware actuators.
* **Replay Protection:** Commands include a monotonic nonce and Unix timestamp. Vehicles reject anything outside a strict 5-second window.
* **Safety Interlocks:** Real cars have hard safety limits. The simulator enforces ISO-26262 interlocks:
  * Rejects opening the frunk or tailgate if ground speed is above 0 km/h
  * Rejects shifting out of park while charging
  * Permits safe actions (like setting a speed limit governor) and synchronizes telemetry immediately

### 4. Road-Following Fleet Simulator
* Vehicles drive along real California routes (San Francisco, Silicon Valley, Monterey, and Lake Tahoe) mapped with OSRM and OpenStreetMap coordinates.
* 14 distinct scenarios are simulated with realistic physics, including highway driving, stop-and-go urban commutes with regenerative braking, heavy towing, cold winter soak, and DC fast supercharging (complete with automated 80% charging knee transitions).

### 5. Mission Control HMI and 3D Digital Twin
* **Rivian OS Design:** Clean typography, Day and Night adaptive themes, a live speed and power arc gauge showing draw and regen, and a battery state-of-charge pill.
* **3D WebGL Vehicle Model (React Three Fiber):**
  * Wheels spin in real time based on live telemetry speed
  * Cargo doors animate open and closed
  * Clickable 3D hotspots allow operators to toggle locks, frunks, and tailgates directly on the car model
* **Optimistic UI Updates:** Actuator buttons highlight immediately when clicked, rolling back cleanly if the vehicle rejects the command due to a safety interlock.
* **Unit System Toggle:** Seamless switching between imperial and metric units (mph/km/h, mi/km, °F/°C, psi/bar).

---

## Tech Stack

* **Backend:** Go 1.24+ (Goroutines, Channels, `net`, `sync`, `crypto/ecdsa`)
* **Serialization:** Google Protocol Buffers (`proto3`)
* **Transport:** Length-Prefixed TCP Sockets, WebSockets, HTTP REST
* **Frontend:** React 18, TypeScript, Vite, Vanilla CSS Design System
* **3D Visualizations:** Three.js, React Three Fiber, Drei
* **Maps & Geo:** Leaflet, OpenStreetMap, OSRM

---

## Running Locally

### Prerequisites
* **Go** (version 1.24 or newer)
* **Bun** (or Node.js 18+ and npm)

### 1. Clone the Repo
```bash
git clone https://github.com/itsnairr/FleetMissionControl.git
cd FleetMissionControl
```

### 2. Start the Gateway Server
The gateway sets up the cryptographic keys, listens for vehicle TCP connections on port 8080, and starts the HTTP/WebSocket API on port 8081:
```bash
go run ./cmd/gateway
```

### 3. Launch the Vehicle Simulator
In another terminal, spin up simulated vehicles driving along California roads:
```bash
# Spawns 10 concurrent connected electric vehicles
go run ./cmd/simulator -n 10
```

### 4. Start the Web Dashboard
In a third terminal, launch the Mission Control interface:
```bash
cd web
bun install     # or npm install
bun run dev     # or npm run dev
```

Open **`http://localhost:5173`** in your browser.

---

## Testing & Quality

Run the Go unit and integration tests (covers framing, cryptographic verification, dispatching, and API handlers):
```bash
go test -v ./...
```

Verify frontend TypeScript types and build the production bundle:
```bash
cd web
bun run build
```

---

## API Summary

### REST Endpoints
* `GET /api/v1/vehicles`: Snapshot of all known vehicles with online status.
* `GET /api/v1/vehicles/{vin}`: Full telemetry state for a specific vehicle.
* `POST /api/v1/vehicles/{vin}/command`: Dispatches a signed command to the vehicle and waits up to 3 seconds for the vehicle's verdict (`200 OK` on accept/reject with reason, `503` if offline, `504` on timeout).

### WebSocket Streams
* `WS /api/v1/fleet/stream`: Live stream of twin state updates across the entire fleet.
* `WS /api/v1/vehicles/{vin}/stream`: High-frequency updates for a single selected vehicle.
* `WS /api/v1/alerts/stream`: Real-time fleet alerts generated by the rules engine and vehicle DTC codes.

---

## Project Structure

```
FleetMissionControl/
├── certs/                      # ECDSA keys for command signing and validation
├── cmd/
│   ├── gateway/                # TCP ingest listener, crypto signer, HTTP/WS server
│   └── simulator/              # Concurrent EV physics and route simulation engine
├── internal/
│   ├── api/                    # REST and WebSocket handlers
│   ├── command/                # Command dispatcher and response registry
│   ├── crypto/                 # ECDSA key generation and signature verification
│   ├── protocol/               # Generated Protobuf Go structs
│   ├── pubsub/                 # In-memory pub/sub fan-out broker
│   ├── rules/                  # Real-time anomaly detection rules engine
│   ├── session/                # Active vehicle TCP connection registry
│   ├── simulator/              # Vehicle state machines, physics, and routes
│   ├── twin/                   # In-memory digital twin registry
│   ├── wire/                   # Length-prefixed framing reader and writer
│   └── worker/                 # Actor-style concurrent telemetry worker pool
├── proto/
│   ├── telemetry.proto         # Protobuf contract for BMS, dynamics, and cabin state
│   └── command.proto           # Protobuf contract for remote vehicle commands
├── tools/
│   └── routegen/               # Route extraction scripts for California highways
└── web/                        # React + TypeScript + Vite operations dashboard
    ├── src/
    │   ├── api/                # Typed REST and WebSocket client
    │   ├── components/         # FleetList, FleetMap, SpeedGauge, VehicleDetail, TopBar
    │   ├── hooks/              # useFleet, useCommands, useAlerts, useTheme
    │   ├── lib/                # Closures, units, model registries, optimistic updates
    │   └── three/              # Three.js / React Three Fiber 3D digital twin
    └── styles.css              # Rivian OS-inspired design system
```

---

## Credits

* 3D Car Models: [Car Kit 3.1](https://kenney.nl/assets/car-kit) by Kenney ([CC0 1.0](https://creativecommons.org/publicdomain/zero/1.0/)).
* Map Tiles: © [OpenStreetMap](https://www.openstreetmap.org/copyright) contributors.
* Road Networks: Generated using [OSRM](https://project-osrm.org).
