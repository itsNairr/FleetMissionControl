package twin

import (
	"sync"

	pb "github.com/itsnairr/fleet-telemetry-engine/internal/protocol"
)

// Provides thread-safe in-memory caching of the latest vehicle state.
type DigitalTwinRegistry struct {
	mu       sync.RWMutex
	vehicles map[string]*pb.VehicleTelemetry
	// Connectivity is tracked separately from telemetry: a telemetry frame still sitting in the worker
	// queue after a disconnect will refresh the snapshot, but can't flip the car back to online.
	online map[string]bool
}

// Initializes an empty vehicle digital twin store.
func NewDigitalTwinRegistry() *DigitalTwinRegistry {
	return &DigitalTwinRegistry{
		vehicles: make(map[string]*pb.VehicleTelemetry),
		online:   make(map[string]bool),
	}
}

func (r *DigitalTwinRegistry) Update(telemetry *pb.VehicleTelemetry) {
	if telemetry == nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	vin := telemetry.GetVin()
	if vin == "" {
		return
	}

	// Keep the twin updated with the latest snapshot
	r.vehicles[vin] = telemetry
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

func (r *DigitalTwinRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.vehicles)
}

// SetOnline records whether the vehicle currently has a live socket to the gateway.
// Offline twins keep their last known state so the dashboard can show "last seen" instead of a vanished car.
func (r *DigitalTwinRegistry) SetOnline(vin string, online bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.online[vin] = online
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
