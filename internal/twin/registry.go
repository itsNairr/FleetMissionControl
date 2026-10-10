package twin

import (
	"sort"
	"sync"

	pb "github.com/itsnairr/fleet-telemetry-engine/internal/protocol"
	"github.com/itsnairr/fleet-telemetry-engine/internal/pubsub"
)

// Snapshot is the twin's full view of one vehicle: its latest telemetry plus whether it is connected
type Snapshot struct {
	Vin       string
	Online    bool
	Telemetry *pb.VehicleTelemetry // Treat as read-only, it is shared between goroutines
}

// Provides thread-safe in-memory caching of the latest vehicle state.
type DigitalTwinRegistry struct {
	mu       sync.RWMutex
	vehicles map[string]*pb.VehicleTelemetry
	// Connectivity is tracked separately from telemetry: a telemetry frame still sitting in the worker
	// queue after a disconnect will refresh the snapshot, but can't flip the car back to online.
	online map[string]bool

	// Every change to a twin is broadcast here so live views (WebSockets) are pushed updates instead of polling
	updates *pubsub.Broker[Snapshot]
}

// Initializes an empty vehicle digital twin store.
func NewDigitalTwinRegistry() *DigitalTwinRegistry {
	return &DigitalTwinRegistry{
		vehicles: make(map[string]*pb.VehicleTelemetry),
		online:   make(map[string]bool),
		updates:  pubsub.NewBroker[Snapshot](256),
	}
}

// Updates exposes the change feed: Subscribe() to receive a Snapshot every time any twin changes
func (r *DigitalTwinRegistry) Updates() *pubsub.Broker[Snapshot] {
	return r.updates
}

func (r *DigitalTwinRegistry) Update(telemetry *pb.VehicleTelemetry) {
	if telemetry == nil {
		return
	}

	vin := telemetry.GetVin()
	if vin == "" {
		return
	}

	// Keep the twin updated with the latest snapshot
	r.mu.Lock()
	r.vehicles[vin] = telemetry
	snap := Snapshot{Vin: vin, Online: r.online[vin], Telemetry: telemetry}
	r.mu.Unlock()

	r.updates.Publish(snap)
}

func (r *DigitalTwinRegistry) Get(vin string) (*pb.VehicleTelemetry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	telemetry, exists := r.vehicles[vin]
	return telemetry, exists
}

func (r *DigitalTwinRegistry) GetAll() []*pb.VehicleTelemetry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	telemetries := make([]*pb.VehicleTelemetry, 0, len(r.vehicles))
	for _, telemetry := range r.vehicles {
		telemetries = append(telemetries, telemetry)
	}
	return telemetries
}

// GetSnapshot returns one vehicle's telemetry + connectivity together
func (r *DigitalTwinRegistry) GetSnapshot(vin string) (Snapshot, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	telemetry, exists := r.vehicles[vin]
	if !exists {
		return Snapshot{}, false
	}
	return Snapshot{Vin: vin, Online: r.online[vin], Telemetry: telemetry}, true
}

// Snapshots returns every known vehicle sorted by VIN (stable ordering for the dashboard)
func (r *DigitalTwinRegistry) Snapshots() []Snapshot {
	r.mu.RLock()
	snaps := make([]Snapshot, 0, len(r.vehicles))
	for vin, telemetry := range r.vehicles {
		snaps = append(snaps, Snapshot{Vin: vin, Online: r.online[vin], Telemetry: telemetry})
	}
	r.mu.RUnlock()

	sort.Slice(snaps, func(i, j int) bool { return snaps[i].Vin < snaps[j].Vin })
	return snaps
}

func (r *DigitalTwinRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.vehicles)
}

// SetOnline records whether the vehicle currently has a live socket to the gateway.
// Offline twins keep their last known state so the dashboard can show "last seen" instead of a vanished car.
func (r *DigitalTwinRegistry) SetOnline(vin string, online bool) {
	r.mu.Lock()
	r.online[vin] = online
	telemetry, known := r.vehicles[vin]
	r.mu.Unlock()

	// Only broadcast once we have telemetry to show (the very first frame arrives via Update right after)
	if known {
		r.updates.Publish(Snapshot{Vin: vin, Online: online, Telemetry: telemetry})
	}
}

func (r *DigitalTwinRegistry) IsOnline(vin string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.online[vin]
}

// Remove removes a vehicle from the registry by VIN.
func (r *DigitalTwinRegistry) Remove(vin string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.vehicles, vin)
	delete(r.online, vin)
}
