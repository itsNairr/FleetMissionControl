package session

//Hashmap to hold the active cars in session

import (
	"fmt"
	"net"
	"sync"

	"github.com/itsnairr/fleet-telemetry-engine/internal/wire"
)

// Session is one live vehicle socket. All writes go through Writer so concurrent commands can't interleave.
type Session struct {
	Conn   net.Conn
	Writer *wire.FrameWriter
}

// Map cars to sockets based off of VIN
type SessionRegistry struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	Quiet    bool // If true, suppresses stdout logging (useful for benchmarks)
}

// NewSessionRegistry initializes a thread-safe vehicle session
func NewSessionRegistry() *SessionRegistry {
	return &SessionRegistry{
		sessions: make(map[string]*Session),
	}
}

// Register vehicle to socket (replaces any stale socket left over from a previous connection)
func (r *SessionRegistry) Register(vin string, conn net.Conn) {
	r.mu.Lock() //Lock the map so no other car can chnage the sessions map or else it will corrupt
	defer r.mu.Unlock() //Unlock
	r.sessions[vin] = &Session{Conn: conn, Writer: wire.NewFrameWriter(conn)} //Add the car to the map
	if !r.Quiet {
		fmt.Printf("[Session] Registered %s | Active Sockets: %d\n", vin, len(r.sessions))
	}
}

// Unregister removes a vehicle socket when the connection closes.
// It only removes the entry if it still belongs to conn: when a car reconnects quickly, the OLD
// connection's cleanup must not delete the NEW session. Returns true if the entry was removed.
func (r *SessionRegistry) Unregister(vin string, conn net.Conn) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, exists := r.sessions[vin]
	if !exists || s.Conn != conn {
		return false
	}
	delete(r.sessions, vin)
	if !r.Quiet {
		fmt.Printf("[Session] Deregistered %s | Active Sockets: %d\n", vin, len(r.sessions))
	}
	return true
}

// Get returns the live session for a specific vehicle, if connected
func (r *SessionRegistry) Get(vin string) (*Session, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, exists := r.sessions[vin]
	return s, exists
}

// Count returns the number of active connected vehicles
func (r *SessionRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.sessions)
}
